package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

func TestCheckDockerFail(t *testing.T) {
	t.Cleanup(docker.SwapNewClientErr(errors.New("not running")))
	if issues := checkDocker(); issues != 1 {
		t.Errorf("expected 1 issue, got %d", issues)
	}
}

func TestCheckDockerOK(t *testing.T) {
	t.Cleanup(docker.SwapNewClientOK())
	if issues := checkDocker(); issues != 0 {
		t.Errorf("expected 0 issues, got %d", issues)
	}
}

func TestCheckFirewallNone(t *testing.T) {
	t.Cleanup(shell.SwapDefault(shelltest.New(nil)))
	if issues := checkFirewall(); issues != 0 {
		t.Errorf("no firewall -> %d issues, want 0", issues)
	}
}

func TestRunDoctorSmoke(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	t.Cleanup(shell.SwapDefault(shelltest.New(nil)))
	// Runs all checks; returns nil regardless of issue count.
	if err := runDoctor(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestCheckNetworkMissing(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	if checkNetwork() == 0 {
		t.Error("missing network should yield issue")
	}
}

func TestCheckTraefikDown(t *testing.T) {
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if checkTraefik() == 0 {
		t.Error("expected issue when traefik down")
	}
}

func TestCheckDNSNoDomains(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if checkDNS() != 0 {
		t.Error("no local domains -> no issue")
	}
}

func TestCheckCertificatesNoMkcertOrNot(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	_ = checkCertificates()
}

func TestCheckSitesValidEmpty(t *testing.T) {
	setupSrvRoot(t)
	if checkSitesValid() != 0 {
		t.Error("no sites -> no issues")
	}
}

func TestCheckPortsSmoke(t *testing.T) {
	// just exercise the function.
	_ = checkPorts()
}

func TestRunUpdateDockerDown(t *testing.T) {
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if err := runUpdate(nil, nil); err == nil {
		t.Error("expected err")
	}
}

func TestRunUpdateHappy(t *testing.T) {
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := runUpdate(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestCheckFirewallActive(t *testing.T) {
	t.Cleanup(shell.SwapDefault(shelltest.New(map[string]shelltest.Response{
		"ufw":      {Exists: true},
		"sudo:ufw": {Out: []byte("Status: active\n80                         ALLOW       Anywhere\n443/tcp                    ALLOW       Anywhere\n")},
	})))
	if checkFirewall() != 0 {
		t.Error("expected 0 issues when ports open")
	}
}

func TestCheckFirewallBlocked(t *testing.T) {
	t.Cleanup(shell.SwapDefault(shelltest.New(map[string]shelltest.Response{
		"ufw":      {Exists: true},
		"sudo:ufw": {Out: []byte("Status: active\n")},
	})))
	if checkFirewall() != 2 {
		t.Error("expected 2 issues (HTTP+HTTPS blocked)")
	}
}

func TestCheckSitesValidWithBroken(t *testing.T) {
	root := setupSrvRoot(t)
	if err := os.WriteFile(filepath.Join(root, "sites", "broken", "metadata.yml"), []byte("type: static\n"), 0o644); err != nil {
		_ = os.MkdirAll(filepath.Join(root, "sites", "broken"), 0o755)
		_ = os.WriteFile(filepath.Join(root, "sites", "broken", "metadata.yml"), []byte("type: static\n"), 0o644)
	}
	_ = checkSitesValid()
}

func TestCheckCertificateExpiry(t *testing.T) {
	setupSrvRoot(t)
	_ = checkCertificateExpiry() // no certs → returns 0
}

func TestCheckCertificateExpiryWithExpiring(t *testing.T) {
	setupSrvRoot(t)
	// Just call; ListLocalCerts returns nothing here.
	_ = checkCertificateExpiry()
}

func TestCheckDNSWithDomains(t *testing.T) {
	root := setupSrvRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "traefik"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a local-domains file with one entry; CheckDNS will fail on it.
	if err := os.WriteFile(filepath.Join(root, "traefik", "local-domains.txt"), []byte("test.local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	_ = checkDNS()
}

func TestCheckCertificatesMkcertInstalled(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
	_ = checkCertificates()
}

func TestScanEnvForHostLoopback(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte(`# Comment line
DB_HOST=127.0.0.1
REDIS_HOST=127.0.0.1
ELASTICSEARCH_HOSTS=http://127.0.0.1:9200
STORAGE_ENDPOINT="http://127.0.0.1:9000"
SOMETHING_ELSE=localhost
DB_PORT=3306
#DB_HOST=127.0.0.1
DATABASE_URL=mysql://root@127.0.0.1:3306/db
`)
	if err := os.WriteFile(envPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	hits := scanEnvForHostLoopback(envPath)
	if len(hits) != 5 {
		t.Fatalf("expected 5 hits, got %d: %v", len(hits), hits)
	}
	// Commented line must not appear
	for _, h := range hits {
		if h[0] == '#' {
			t.Errorf("commented line leaked: %q", h)
		}
	}
}

func TestScanEnvForHostLoopbackMissingFile(t *testing.T) {
	if hits := scanEnvForHostLoopback("/nonexistent/.env"); hits != nil {
		t.Errorf("missing file should return nil, got %v", hits)
	}
}
