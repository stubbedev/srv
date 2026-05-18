package traefik

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/mkcert"
)

type caRootStub struct {
	out []byte
}

func (caRootStub) Stream(args ...string) error               { return nil }
func (s caRootStub) Output(args ...string) ([]byte, error)   { return s.out, nil }
func (caRootStub) Combined(args ...string) ([]byte, error)   { return nil, nil }

func mkcertSwapRunner(r mkcert.CommandRunner) func() {
	return mkcert.SwapRunner(r)
}

// writePEMCert produces a self-signed cert with the supplied SANs and writes
// it to certPath in PEM form. Lifetime can be set via the validity offsets
// (notBefore = now+notBeforeOffset, notAfter = now+notAfterOffset).
func writePEMCert(t *testing.T, certPath string, sans []string, notBeforeOffset, notAfterOffset time.Duration) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: sans[0]},
		NotBefore:    time.Now().Add(notBeforeOffset),
		NotAfter:     time.Now().Add(notAfterOffset),
		DNSNames:     sans,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := pem.Encode(out, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
}

func setupSrvRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	return root
}

func TestParseCertFileMissing(t *testing.T) {
	info := parseCertFile("/no/such/cert.crt")
	if info.Exists || info.Corrupt {
		t.Errorf("missing -> %+v, want Exists=false Corrupt=false", info)
	}
}

func TestParseCertFileCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/bad.crt"
	if err := os.WriteFile(path, []byte("not pem"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := parseCertFile(path)
	if info.Exists || !info.Corrupt {
		t.Errorf("corrupt -> %+v, want Corrupt=true", info)
	}
}

func TestParseCertFileBadDER(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/bad.crt"
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage")})
	if err := os.WriteFile(path, pemBlock, 0o644); err != nil {
		t.Fatal(err)
	}
	info := parseCertFile(path)
	if !info.Corrupt {
		t.Errorf("bad DER should be Corrupt: %+v", info)
	}
}

func TestParseCertFileValid(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/ok.crt"
	writePEMCert(t, path, []string{"foo.local"}, -time.Hour, 90*24*time.Hour)
	info := parseCertFile(path)
	if !info.Exists {
		t.Fatal("expected Exists=true")
	}
	if info.IsExpired {
		t.Error("should not be expired")
	}
	if info.DaysLeft < 80 || info.DaysLeft > 100 {
		t.Errorf("DaysLeft = %d, want ~90", info.DaysLeft)
	}
}

func TestParseCertFileExpired(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/exp.crt"
	writePEMCert(t, path, []string{"x.local"}, -48*time.Hour, -24*time.Hour)
	info := parseCertFile(path)
	if !info.IsExpired {
		t.Errorf("should be expired: %+v", info)
	}
}

func TestLocalCertsExist(t *testing.T) {
	root := setupSrvRoot(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if LocalCertsExist("blog", "blog.local") {
		t.Error("should be false initially")
	}
	certDir := cfg.SiteCertsDir("blog")
	_ = os.MkdirAll(certDir, 0o755)
	if err := os.WriteFile(filepath.Join(certDir, "blog.local.crt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certDir, "blog.local.key"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !LocalCertsExist("blog", "blog.local") {
		t.Error("should be true after both files written")
	}
	_ = root
}

func TestRemoveLocalCertsMissing(t *testing.T) {
	setupSrvRoot(t)
	if err := RemoveLocalCerts("ghost", "ghost.local"); err != nil {
		t.Errorf("Remove on missing -> %v", err)
	}
}

func TestRemoveLocalCertsKeyRemoveErrors(t *testing.T) {
	// Force os.Remove to fail by making the key a directory.
	setupSrvRoot(t)
	cfg, _ := config.Load()
	certDir := cfg.SiteCertsDir("blog")
	_ = os.MkdirAll(certDir, 0o755)
	// Create a directory at the cert path so Remove fails with non-NotExist err.
	if err := os.MkdirAll(filepath.Join(certDir, "blog.local.crt", "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLocalCerts("blog", "blog.local"); err == nil {
		t.Error("expected err from non-empty cert path")
	}
}

func TestIsCAInstalledNoCAROOTFile(t *testing.T) {
	stub := &caRootStub{out: []byte("/tmp/nonexistent-srv-test")}
	t.Cleanup(mkcertSwapRunner(stub))
	if IsCAInstalled() {
		t.Error("CA file missing → expected false")
	}
}

func TestIsCAInstalledEmptyCAROOT(t *testing.T) {
	stub := &caRootStub{out: []byte("  \n")}
	t.Cleanup(mkcertSwapRunner(stub))
	if IsCAInstalled() {
		t.Error("empty CAROOT → expected false")
	}
}

func TestRemoveLocalCertsExisting(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	certDir := cfg.SiteCertsDir("blog")
	_ = os.MkdirAll(certDir, 0o755)
	certPath := filepath.Join(certDir, "blog.local.crt")
	keyPath := filepath.Join(certDir, "blog.local.key")
	_ = os.WriteFile(certPath, []byte("x"), 0o644)
	_ = os.WriteFile(keyPath, []byte("x"), 0o644)
	if err := RemoveLocalCerts("blog", "blog.local"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(certPath); !os.IsNotExist(err) {
		t.Error("cert should be gone")
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Error("key should be gone")
	}
}

func TestCertCoversDomains(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	certDir := cfg.SiteCertsDir("blog")
	certPath := filepath.Join(certDir, "blog.local.crt")
	writePEMCert(t, certPath, []string{"blog.local", "*.blog.local"}, -time.Hour, 30*24*time.Hour)

	if !certCoversDomains("blog", "blog.local", []string{"blog.local"}, false) {
		t.Error("should cover apex")
	}
	if !certCoversDomains("blog", "blog.local", []string{"blog.local"}, true) {
		t.Error("should cover wildcard when present")
	}
	if certCoversDomains("blog", "blog.local", []string{"missing.local"}, false) {
		t.Error("should not cover unrelated domain")
	}
}

func TestCertCoversDomainsMissingFile(t *testing.T) {
	setupSrvRoot(t)
	if certCoversDomains("blog", "blog.local", []string{"blog.local"}, false) {
		t.Error("missing cert should not cover")
	}
}

func TestGetLocalCertInfo(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	certPath := filepath.Join(cfg.SiteCertsDir("blog"), "blog.local.crt")
	writePEMCert(t, certPath, []string{"blog.local"}, -time.Hour, 90*24*time.Hour)
	info := GetLocalCertInfo("blog", "blog.local")
	if !info.Exists {
		t.Fatal("expected Exists=true")
	}
	if info.SiteName != "blog" || info.Domain != "blog.local" {
		t.Errorf("identity: %+v", info)
	}
}

func TestScanSiteCertificatesEmpty(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	certs, err := scanSiteCertificates(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(certs) != 0 {
		t.Errorf("expected 0, got %v", certs)
	}
}

func TestScanSiteCertificatesFindsPair(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	certDir := cfg.SiteCertsDir("blog")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certDir, "blog.local.crt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certDir, "blog.local.key"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	certs, err := scanSiteCertificates(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1, got %v", certs)
	}
	if certs[0].siteName != "blog" || certs[0].domain != "blog.local" {
		t.Errorf("got %+v", certs[0])
	}
}

func TestScanSiteCertificatesSkipsKeyWithoutCert(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	certDir := cfg.SiteCertsDir("blog")
	_ = os.MkdirAll(certDir, 0o755)
	_ = os.WriteFile(filepath.Join(certDir, "blog.local.crt"), []byte("x"), 0o644)
	// No key file → skipped.
	certs, _ := scanSiteCertificates(cfg)
	if len(certs) != 0 {
		t.Errorf("expected 0 when key missing, got %v", certs)
	}
}

func TestUpdateDynamicConfigEmpty(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := UpdateDynamicConfig(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.TraefikConfDir(), "traefik-dynamic.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), "certificates: []") {
		t.Errorf("expected empty cert list: %q", string(data))
	}
}

func TestUpdateDynamicConfigWithCerts(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	_ = os.MkdirAll(cfg.TraefikConfDir(), 0o755)
	certDir := cfg.SiteCertsDir("blog")
	_ = os.MkdirAll(certDir, 0o755)
	_ = os.WriteFile(filepath.Join(certDir, "blog.local.crt"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(certDir, "blog.local.key"), []byte("x"), 0o644)
	if err := UpdateDynamicConfig(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(cfg.TraefikConfDir(), "traefik-dynamic.yml"))
	if !contains(string(data), "blog.local.crt") {
		t.Errorf("expected cert reference in output: %q", string(data))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(s) > 0 && (find(s, sub) >= 0)))
}

func find(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
