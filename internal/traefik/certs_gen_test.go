package traefik

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/mkcert"
)

func mustLoadCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestCheckMkcertAvailable(t *testing.T) {
	// TestMain points CAROOT into the temp SRV_ROOT, so the vendored engine
	// is available without any host dependency.
	if err := CheckMkcert(); err != nil {
		t.Errorf("CheckMkcert() = %v, want nil", err)
	}
}

func TestCheckMkcertNoCAROOT(t *testing.T) {
	t.Cleanup(mkcert.SwapEngine(mkcert.SystemEngine()))
	t.Setenv("CAROOT", "")
	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	if err := CheckMkcert(); err == nil {
		t.Error("expected err when no CA directory can be resolved")
	}
}

func TestIsCAInstalledTracksRootCAFile(t *testing.T) {
	caroot := t.TempDir()
	t.Setenv("CAROOT", caroot)
	if IsCAInstalled() {
		t.Error("empty CAROOT should not count as installed")
	}
	if err := os.WriteFile(filepath.Join(caroot, "rootCA.pem"), []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !IsCAInstalled() {
		t.Error("existing rootCA.pem should count as installed")
	}
}

func TestInstallCA(t *testing.T) {
	engine := &stubMkcertEngine{}
	t.Cleanup(mkcert.SwapEngine(engine))
	res, err := InstallCA()
	if err != nil {
		t.Fatal(err)
	}
	if !res.NewCA {
		t.Error("NewCA flag missing")
	}
	if !res.SystemTrustOK {
		t.Error("SystemTrustOK flag missing")
	}
}

func TestInstallCAErr(t *testing.T) {
	engine := &stubMkcertEngine{installErr: errors.New("exit 1")}
	t.Cleanup(mkcert.SwapEngine(engine))
	_, err := InstallCA()
	if err == nil {
		t.Error("expected err")
	}
}

func TestGenerateLocalCertNoDomains(t *testing.T) {
	if err := GenerateLocalCert("blog", nil, false); err == nil {
		t.Error("expected err for empty domains")
	}
}

func TestGenerateLocalCertSuccess(t *testing.T) {
	setupSrvRoot(t)
	if err := GenerateLocalCert("blog", []string{"blog.local"}, false); err != nil {
		t.Fatal(err)
	}
	if !LocalCertsExist("blog", "blog.local") {
		t.Error("expected cert and key on disk")
	}
}

func TestGenerateLocalCertWildcardAddsSAN(t *testing.T) {
	setupSrvRoot(t)
	if err := GenerateLocalCert("blog", []string{"blog.com"}, true); err != nil {
		t.Fatal(err)
	}
	cert := parseLocalCert(t, "blog", "blog.com")
	want := []string{"blog.com", "*.blog.com"}
	if len(cert.DNSNames) != len(want) {
		t.Fatalf("DNSNames = %v, want %v", cert.DNSNames, want)
	}
	for _, name := range want {
		if err := cert.VerifyHostname(name); err != nil {
			t.Errorf("cert does not cover %q (SANs: %v): %v", name, cert.DNSNames, err)
		}
	}
}

func TestGenerateLocalCertInvalidDomain(t *testing.T) {
	setupSrvRoot(t)
	if err := GenerateLocalCert("blog", []string{"not a domain!"}, false); err == nil {
		t.Error("expected err for invalid hostname")
	}
}

func TestGenerateLocalCertEngineErr(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(mkcert.SwapEngine(&stubMkcertEngine{issueErr: errors.New("exit 1")}))
	if err := GenerateLocalCert("blog", []string{"blog.local"}, false); err == nil {
		t.Error("expected err")
	}
}

func TestEnsureLocalCertNoDomains(t *testing.T) {
	if _, err := EnsureLocalCert("blog", nil, false); err == nil {
		t.Error("expected err")
	}
}

func TestEnsureLocalCertGeneratesWhenMissing(t *testing.T) {
	setupSrvRoot(t)
	renewed, err := EnsureLocalCert("blog", []string{"blog.local"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !renewed {
		t.Error("expected renewed=true")
	}
}

func TestEnsureLocalCertSkipsWhenCovered(t *testing.T) {
	setupSrvRoot(t)
	// Generate first.
	if _, err := EnsureLocalCert("blog", []string{"blog.local"}, false); err != nil {
		t.Fatal(err)
	}

	// Write a real cert covering the domain so EnsureLocalCert can verify SAN.
	cfg := mustLoadCfg(t)
	certPath := cfg.SiteCertsDir("blog") + "/blog.local.crt"
	writePEMCert(t, certPath, []string{"blog.local"}, -time.Hour, 90*24*time.Hour)
	// Also create the key so LocalCertsExist returns true.
	keyPath := cfg.SiteCertsDir("blog") + "/blog.local.key"
	if writeErr := writePEMKey(keyPath); writeErr != nil {
		t.Fatal(writeErr)
	}

	renewed, err := EnsureLocalCert("blog", []string{"blog.local"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if renewed {
		t.Error("should not regenerate when cert covers domain")
	}
}

func TestListLocalCertsEmpty(t *testing.T) {
	setupSrvRoot(t)
	if got := ListLocalCerts(); len(got) != 0 {
		t.Errorf("expected 0, got %v", got)
	}
}

func TestListLocalCertsFinds(t *testing.T) {
	setupSrvRoot(t)
	cfg := mustLoadCfg(t)
	certDir := cfg.SiteCertsDir("blog")
	writePEMCert(t, certDir+"/blog.local.crt", []string{"blog.local"}, -time.Hour, 30*24*time.Hour)
	if err := writePEMKey(certDir + "/blog.local.key"); err != nil {
		t.Fatal(err)
	}
	certs := ListLocalCerts()
	if len(certs) != 1 {
		t.Fatalf("expected 1, got %v", certs)
	}
	if certs[0].SiteName != "blog" || certs[0].Domain != "blog.local" {
		t.Errorf("got %+v", certs[0])
	}
}

// writePEMKey writes a minimal placeholder so cert-existence checks pass.
func writePEMKey(path string) error {
	return os.WriteFile(path, []byte("-----BEGIN PRIVATE KEY-----\nMINIMAL\n-----END PRIVATE KEY-----\n"), 0o644)
}
