package traefik

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/fsutil"
	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/validate"
)

// CheckMkcert verifies mkcert is available on $PATH.
func CheckMkcert() error {
	if !mkcert.Available() {
		return fmt.Errorf("mkcert not found on $PATH. Install it: `brew install mkcert` / `nix profile install nixpkgs#mkcert` / your distro package manager")
	}
	return nil
}

// IsCAInstalled checks if the mkcert CA is installed.
func IsCAInstalled() bool {
	output, err := mkcert.Output("-CAROOT")
	if err != nil {
		return false
	}
	caRoot := strings.TrimSpace(string(output))
	if caRoot == "" {
		return false
	}
	_, err = os.Stat(filepath.Join(caRoot, constants.RootCAFile))
	return err == nil
}

// InstallCA installs the mkcert CA certificate. mkcert's output is captured
// and returned as a parsed result so callers can render a clean message rather
// than leaking mkcert's raw multi-line warnings.
func InstallCA() (mkcert.InstallResult, error) {
	res, err := mkcert.Install()
	if err != nil {
		return res, fmt.Errorf("failed to install mkcert CA: %w", err)
	}
	return res, nil
}

// LocalCertsExist checks if local SSL certificates exist for a site.
func LocalCertsExist(siteName, domain string) bool {
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	certFile := filepath.Join(cfg.SiteCertsDir(siteName), domain+constants.ExtCert)
	keyFile := filepath.Join(cfg.SiteCertsDir(siteName), domain+constants.ExtKey)
	_, certErr := os.Stat(certFile)
	_, keyErr := os.Stat(keyFile)
	return certErr == nil && keyErr == nil
}

// RemoveLocalCerts removes SSL certificates for a specific site.
// Returns an error if removal fails for files that exist.
func RemoveLocalCerts(siteName, domain string) error {
	if err := validate.NoTraversal(siteName); err != nil {
		return err
	}
	if err := validate.NoTraversal(domain); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	certFile := filepath.Join(cfg.SiteCertsDir(siteName), domain+constants.ExtCert)
	keyFile := filepath.Join(cfg.SiteCertsDir(siteName), domain+constants.ExtKey)

	var errs []error
	if err := os.Remove(certFile); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("failed to remove cert file: %w", err))
	}
	if err := os.Remove(keyFile); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("failed to remove key file: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to remove certificates: %v", errs)
	}
	return nil
}

// GenerateLocalCert generates an SSL certificate for a site using mkcert.
// The first element of domains is the primary (used to name the cert files on
// disk); all elements are added as SANs. When wildcard is true, each domain
// also gets a "*.<domain>" SAN so single-level subdomains are covered.
func GenerateLocalCert(siteName string, domains []string, wildcard bool) error {
	if len(domains) == 0 {
		return fmt.Errorf("no domains supplied for cert generation")
	}
	if err := validate.NoTraversal(siteName); err != nil {
		return err
	}
	for _, d := range domains {
		if err := validate.NoTraversal(d); err != nil {
			return err
		}
	}
	if err := CheckMkcert(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// 0700: the directory holds private keys. mkcert writes the *.key files
	// 0600, but a private cert dir keeps the .crt files and the listing itself
	// from being world-readable too.
	certDir := cfg.SiteCertsDir(siteName)
	if err := os.MkdirAll(certDir, constants.DirPermPrivate); err != nil {
		return fmt.Errorf("failed to create certs directory: %w", err)
	}

	primary := domains[0]
	certFile := filepath.Join(certDir, primary+constants.ExtCert)
	keyFile := filepath.Join(certDir, primary+constants.ExtKey)

	args := []string{
		"-cert-file", certFile,
		"-key-file", keyFile,
	}
	for _, d := range domains {
		args = append(args, d)
		if wildcard {
			args = append(args, "*."+d)
		}
	}

	// RunQuiet suppresses mkcert's advisory stderr warnings (e.g. the "not
	// installed in system trust store" note that fires immediately after
	// install due to mkcert's cached cert pool — see FiloSottile/mkcert#234).
	if err := mkcert.RunQuiet(args...); err != nil {
		return fmt.Errorf("failed to generate certificate for %s: %w", primary, err)
	}

	return nil
}

// RenewThresholdDays is the number of days before expiry to trigger auto-renewal.
const RenewThresholdDays = constants.CertExpiryWarningDays

// EnsureResourceCert ensures mkcert is available, the local CA is installed,
// and a cert exists for (siteName, domain). It is the headless core shared by
// `srv proxy add` / `srv redirect add` (which wrap it with CLI progress
// reporting) and the MCP add_proxy / add_redirect tools. Returns whether a cert
// was (re)issued.
func EnsureResourceCert(siteName, domain string, wildcard bool) (renewed bool, err error) {
	if err := CheckMkcert(); err != nil {
		return false, err
	}
	if !IsCAInstalled() {
		if _, err := InstallCA(); err != nil {
			return false, fmt.Errorf("failed to install mkcert CA: %w", err)
		}
	}
	renewed, err = EnsureLocalCert(siteName, []string{domain}, wildcard)
	if err != nil {
		return false, fmt.Errorf("failed to generate certificate: %w", err)
	}
	return renewed, nil
}

// EnsureLocalCert generates an SSL certificate for a site if it doesn't exist
// or if the existing certificate is expired, expiring soon, missing the
// requested wildcard SAN, or missing one of the requested domains.
// Returns (renewed bool, err error) where renewed indicates if a cert was regenerated.
func EnsureLocalCert(siteName string, domains []string, wildcard bool) (bool, error) {
	if len(domains) == 0 {
		return false, fmt.Errorf("no domains supplied")
	}
	primary := domains[0]

	if !LocalCertsExist(siteName, primary) {
		return true, GenerateLocalCert(siteName, domains, wildcard)
	}

	// Check if cert needs renewal (also regenerate when the file is unparseable —
	// the cert.Corrupt path catches truncated/damaged files that LocalCertsExist
	// can't detect by stat alone).
	cert := GetLocalCertInfo(siteName, primary)
	if cert.Corrupt || cert.IsExpired || cert.DaysLeft <= RenewThresholdDays {
		return true, GenerateLocalCert(siteName, domains, wildcard)
	}

	// Upgrade if SAN coverage is incomplete (missing wildcard or any extra domain).
	if !certCoversDomains(siteName, primary, domains, wildcard) {
		return true, GenerateLocalCert(siteName, domains, wildcard)
	}

	return false, nil
}

// certCoversDomains reports whether the on-disk cert (named after primary)
// includes every required domain (and `*.<d>` if wildcard) as a SAN.
func certCoversDomains(siteName, primary string, domains []string, wildcard bool) bool {
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	certPath := filepath.Join(cfg.SiteCertsDir(siteName), primary+constants.ExtCert)
	data, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return false
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	have := make(map[string]bool, len(parsed.DNSNames))
	for _, n := range parsed.DNSNames {
		have[n] = true
	}
	for _, d := range domains {
		if !have[d] {
			return false
		}
		if wildcard && !have["*."+d] {
			return false
		}
	}
	return true
}

// UpdateDynamicConfig regenerates the Traefik dynamic config with all local domain certs.
// It scans all site directories for certificates.
func UpdateDynamicConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Find all certificates across all site directories
	certs, err := scanSiteCertificates(cfg)
	if err != nil {
		return err
	}

	// Write atomically so Traefik (which watches this file) never reads a
	// partial/truncated config between the truncate and the final write.
	dynamicPath := filepath.Join(cfg.TraefikConfDir(), "traefik-dynamic.yml")
	if err := fsutil.AtomicWriteFile(dynamicPath, []byte(renderDynamicConfig(certs)), constants.FilePermDefault); err != nil {
		return fmt.Errorf("failed to write dynamic config: %w", err)
	}

	return nil
}

// tlsCertificate is one certFile/keyFile pair in the Traefik dynamic config.
type tlsCertificate struct {
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
}

// dynamicConfig models traefik-dynamic.yml. A nil Certificates slice marshals
// as `certificates: []`, which is the correct empty form for the base config
// written at install time.
type dynamicConfig struct {
	TLS struct {
		Certificates []tlsCertificate `yaml:"certificates"`
	} `yaml:"tls"`
}

// renderDynamicConfig builds traefik-dynamic.yml from the discovered certs.
// Both the install-time base config (certs == nil) and the live regeneration
// go through here so the file shape stays consistent. The cert/key paths are
// the in-container mount paths: /etc/traefik/sites/{site}/certs/{domain}.{crt,key}.
//
// The marshal error is ignored: dynamicConfig is a fixed-shape struct of
// strings, which yaml.Marshal cannot fail to encode.
func renderDynamicConfig(certs []certEntry) string {
	var doc dynamicConfig
	for _, cert := range certs {
		doc.TLS.Certificates = append(doc.TLS.Certificates, tlsCertificate{
			CertFile: fmt.Sprintf("%s/%s/%s/%s%s",
				constants.TraefikContainerSitesPath, cert.siteName,
				constants.TraefikContainerCertsSubdir, cert.domain, constants.ExtCert),
			KeyFile: fmt.Sprintf("%s/%s/%s/%s%s",
				constants.TraefikContainerSitesPath, cert.siteName,
				constants.TraefikContainerCertsSubdir, cert.domain, constants.ExtKey),
		})
	}
	data, _ := yaml.Marshal(&doc) //nolint:errcheck // static struct never fails to marshal
	return "# Dynamic Traefik configuration - generated by srv\n# Do not edit manually\n" + string(data)
}

// certEntry represents a certificate file pair for a site.
type certEntry struct {
	siteName string
	domain   string
}

// scanSiteCertificates scans all site directories for certificate files.
// Returns a list of certEntry with siteName and domain for each valid cert/key pair.
func scanSiteCertificates(cfg *config.Config) ([]certEntry, error) {
	var certs []certEntry

	// Scan sites directory
	siteEntries, err := os.ReadDir(cfg.SitesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return certs, nil
		}
		return nil, fmt.Errorf("failed to read sites directory: %w", err)
	}

	for _, siteEntry := range siteEntries {
		if !siteEntry.IsDir() {
			continue
		}
		siteName := siteEntry.Name()
		certDir := cfg.SiteCertsDir(siteName)

		certFiles, err := os.ReadDir(certDir)
		if err != nil {
			continue // No certs dir for this site
		}

		for _, certFile := range certFiles {
			if certFile.IsDir() {
				continue
			}
			name := certFile.Name()
			if before, ok := strings.CutSuffix(name, constants.ExtCert); ok {
				domain := before
				keyFile := filepath.Join(certDir, domain+constants.ExtKey)
				if _, err := os.Stat(keyFile); err == nil {
					certs = append(certs, certEntry{siteName: siteName, domain: domain})
				}
			}
		}
	}

	return certs, nil
}

// CertInfo holds certificate expiry information.
type CertInfo struct {
	SiteName  string
	Domain    string
	Exists    bool
	ExpiresAt time.Time
	DaysLeft  int
	IsExpired bool
	// Corrupt is true when a cert file is present on disk but cannot be parsed
	// as a valid X.509 certificate (bad PEM, truncated bytes, etc.). Callers
	// can use this to surface the corruption to the user instead of treating
	// it as "just regenerate quietly."
	Corrupt bool
}

// CertStatus is the lifecycle state of a local certificate, used by list and
// doctor views.
type CertStatus string

// Certificate lifecycle states. Order of precedence is corrupt > missing >
// expired > expiring > valid (see Status).
const (
	CertStatusCorrupt  CertStatus = "corrupt"
	CertStatusMissing  CertStatus = "missing"
	CertStatusExpired  CertStatus = "expired"
	CertStatusExpiring CertStatus = "expiring"
	CertStatusValid    CertStatus = "valid"
)

// Status classifies the certificate into a single lifecycle state. It is the
// one place this precedence lives — list, inspect, and doctor all call it
// instead of re-deriving the same switch.
func (c CertInfo) Status() CertStatus {
	switch {
	case c.Corrupt:
		return CertStatusCorrupt
	case !c.Exists:
		return CertStatusMissing
	case c.IsExpired:
		return CertStatusExpired
	case c.DaysLeft <= constants.CertExpiryWarningDays:
		return CertStatusExpiring
	default:
		return CertStatusValid
	}
}

// GetLocalCertInfo returns information about a specific site's SSL certificate.
func GetLocalCertInfo(siteName, domain string) CertInfo {
	cfg, err := config.Load()
	if err != nil {
		return CertInfo{SiteName: siteName, Domain: domain}
	}

	certFile := filepath.Join(cfg.SiteCertsDir(siteName), domain+constants.ExtCert)
	info := parseCertFile(certFile)
	info.SiteName = siteName
	info.Domain = domain
	return info
}

// ListLocalCerts returns information about all local SSL certificates across all sites.
func ListLocalCerts() []CertInfo {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}

	certs, err := scanSiteCertificates(cfg)
	if err != nil {
		return nil
	}

	var certInfos []CertInfo
	for _, cert := range certs {
		certInfos = append(certInfos, GetLocalCertInfo(cert.siteName, cert.domain))
	}

	return certInfos
}

// parseCertFile reads a PEM certificate file and returns expiry info.
//
// Three outcomes:
//   - file missing → Exists=false, Corrupt=false (the normal "issue a new cert" path)
//   - file present but unparseable → Exists=false, Corrupt=true (the user should know)
//   - valid cert → Exists=true, with ExpiresAt/DaysLeft/IsExpired populated
func parseCertFile(certPath string) CertInfo {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return CertInfo{Exists: false}
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return CertInfo{Exists: false, Corrupt: true}
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return CertInfo{Exists: false, Corrupt: true}
	}

	now := time.Now()
	daysLeft := int(cert.NotAfter.Sub(now).Hours() / constants.HoursPerDay)

	return CertInfo{
		Exists:    true,
		ExpiresAt: cert.NotAfter,
		DaysLeft:  daysLeft,
		IsExpired: now.After(cert.NotAfter),
	}
}
