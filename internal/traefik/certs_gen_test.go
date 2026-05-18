package traefik

import (
	"errors"
	"os"
	"strings"
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

// mkcertStub implements mkcert.CommandRunner. Records calls; configurable err.
type mkcertStub struct {
	streamErr error
	outErr    error
	combErr   error
	calls     []string
}

func (m *mkcertStub) Stream(args ...string) error {
	m.calls = append(m.calls, "Stream:"+strings.Join(args, ","))
	return m.streamErr
}
func (m *mkcertStub) Output(args ...string) ([]byte, error) {
	m.calls = append(m.calls, "Output:"+strings.Join(args, ","))
	return []byte("/root/mkcert\n"), m.outErr
}
func (m *mkcertStub) Combined(args ...string) ([]byte, error) {
	m.calls = append(m.calls, "Combined:"+strings.Join(args, ","))
	return []byte("Created a new local CA"), m.combErr
}

func TestCheckMkcertAvailable(t *testing.T) {
	// We can't make mkcert.Available() return false without unloading the
	// binary. Just call to exercise the path.
	_ = CheckMkcert()
}

func TestIsCAInstalledOutputErr(t *testing.T) {
	stub := &mkcertStub{outErr: errors.New("missing binary")}
	t.Cleanup(mkcert.SwapRunner(stub))
	if IsCAInstalled() {
		t.Error("err should yield false")
	}
}

func TestInstallCA(t *testing.T) {
	stub := &mkcertStub{}
	t.Cleanup(mkcert.SwapRunner(stub))
	res, err := InstallCA()
	if err != nil {
		t.Fatal(err)
	}
	if !res.NewCA {
		t.Error("NewCA flag missing")
	}
}

func TestInstallCAErr(t *testing.T) {
	stub := &mkcertStub{combErr: errors.New("exit 1")}
	t.Cleanup(mkcert.SwapRunner(stub))
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
	stub := &mkcertStub{}
	t.Cleanup(mkcert.SwapRunner(stub))
	if err := GenerateLocalCert("blog", []string{"blog.local"}, false); err != nil {
		t.Fatal(err)
	}
	if len(stub.calls) == 0 {
		t.Error("expected mkcert call")
	}
}

func TestGenerateLocalCertWildcardAddsSAN(t *testing.T) {
	setupSrvRoot(t)
	stub := &mkcertStub{}
	t.Cleanup(mkcert.SwapRunner(stub))
	if err := GenerateLocalCert("blog", []string{"blog.com"}, true); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(stub.calls, " ")
	if !strings.Contains(joined, "*.blog.com") {
		t.Errorf("wildcard SAN missing: %v", stub.calls)
	}
}

func TestGenerateLocalCertMkcertErr(t *testing.T) {
	setupSrvRoot(t)
	stub := &mkcertStub{outErr: errors.New("exit 1")}
	t.Cleanup(mkcert.SwapRunner(stub))
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
	stub := &mkcertStub{}
	t.Cleanup(mkcert.SwapRunner(stub))
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
	stub := &mkcertStub{}
	t.Cleanup(mkcert.SwapRunner(stub))
	// Generate first.
	if _, err := EnsureLocalCert("blog", []string{"blog.local"}, false); err != nil {
		t.Fatal(err)
	}
	stub.calls = nil

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
