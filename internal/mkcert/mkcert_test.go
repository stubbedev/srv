package mkcert

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// carootSandbox points CAROOT at a fresh temp dir for the duration of the
// test, so CA files never land in the developer's real ~/.local/share/mkcert.
func carootSandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CAROOT", dir)
	return dir
}

func TestCAROOTEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CAROOT", dir)
	if got := CAROOT(); got != dir {
		t.Errorf("CAROOT() = %q, want %q", got, dir)
	}
}

func TestCAROOTUnixDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix layout")
	}
	t.Setenv("CAROOT", "")
	t.Setenv("XDG_DATA_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".local", "share", "mkcert")
	if got := CAROOT(); got != want {
		t.Errorf("CAROOT() = %q, want %q", got, want)
	}
}

func TestCAROOTXDGDataHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix layout")
	}
	t.Setenv("CAROOT", "")
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	want := filepath.Join(xdg, "mkcert")
	if got := CAROOT(); got != want {
		t.Errorf("CAROOT() = %q, want %q", got, want)
	}
}

func TestAvailableRequiresCAROOT(t *testing.T) {
	carootSandbox(t)
	if !Available() {
		t.Error("resolvable CAROOT should mean available")
	}
	t.Setenv("CAROOT", "")
	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	if Available() {
		t.Error("no resolvable CA directory should mean unavailable")
	}
}

func TestLoadCACreatesCA(t *testing.T) {
	dir := carootSandbox(t)

	ca, err := loadCA()
	if err != nil {
		t.Fatal(err)
	}
	if !ca.created {
		t.Error("first load should create the CA")
	}
	if !ca.caCert.IsCA {
		t.Error("generated certificate is not a CA")
	}
	if ca.caKey == nil {
		t.Fatal("CA key missing")
	}

	if _, err := os.Stat(filepath.Join(dir, rootName)); err != nil {
		t.Errorf("rootCA.pem missing: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	keyInfo, err := os.Stat(filepath.Join(dir, rootKeyName))
	if err != nil {
		t.Fatal(err)
	}
	if got := keyInfo.Mode().Perm(); got != 0o400 {
		t.Errorf("CA key perms = %o, want 400", got)
	}
}

func TestLoadCAReusesExisting(t *testing.T) {
	carootSandbox(t)
	first, err := loadCA()
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadCA()
	if err != nil {
		t.Fatal(err)
	}
	if second.created {
		t.Error("second load must reuse the existing CA")
	}
	if !first.caCert.Equal(second.caCert) {
		t.Error("CA certificate changed between loads")
	}
}

func TestIssueCertSANsAndValidity(t *testing.T) {
	carootSandbox(t)
	certPath := filepath.Join(t.TempDir(), "app.test.crt")
	keyPath := filepath.Join(t.TempDir(), "app.test.key")

	domains := []string{"app.test", "*.app.test", "127.0.0.1"}
	if err := IssueCert(certPath, keyPath, domains); err != nil {
		t.Fatal(err)
	}

	cert := readCert(t, certPath)
	for _, name := range []string{"app.test", "*.app.test"} {
		if err := cert.VerifyHostname(name); err != nil {
			t.Errorf("cert does not cover %q: %v", name, err)
		}
	}
	if len(cert.IPAddresses) != 1 || cert.IPAddresses[0].String() != "127.0.0.1" {
		t.Errorf("IP SANs = %v, want [127.0.0.1]", cert.IPAddresses)
	}
	validity := cert.NotAfter.Sub(cert.NotBefore)
	// 2 years and 3 months, within a day of drift.
	if validity < 815*24*time.Hour || validity > 827*24*time.Hour {
		t.Errorf("validity = %v, want ~825 days (macOS limit)", validity)
	}
	if cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Error("missing digital signature key usage")
	}
}

func TestIssueCertFilePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	carootSandbox(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "app.test.crt")
	keyPath := filepath.Join(dir, "app.test.key")
	if err := IssueCert(certPath, keyPath, []string{"app.test"}); err != nil {
		t.Fatal(err)
	}
	certInfo, err := os.Stat(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := certInfo.Mode().Perm(); got != 0o644 {
		t.Errorf("cert perms = %o, want 644", got)
	}
	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := keyInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("key perms = %o, want 600", got)
	}
}

func TestIssueCertSignedByLocalCA(t *testing.T) {
	dir := carootSandbox(t)
	certPath := filepath.Join(t.TempDir(), "app.test.crt")
	if err := IssueCert(certPath, filepath.Join(t.TempDir(), "app.test.key"), []string{"app.test"}); err != nil {
		t.Fatal(err)
	}

	caPEM, err := os.ReadFile(filepath.Join(dir, rootName))
	if err != nil {
		t.Fatal(err)
	}
	caBlock, _ := pem.Decode(caPEM)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	leaf := readCert(t, certPath)
	if err := leaf.CheckSignatureFrom(caCert); err != nil {
		t.Errorf("leaf is not signed by the local CA: %v", err)
	}
}

func TestIssueCertInvalidHostname(t *testing.T) {
	carootSandbox(t)
	err := IssueCert(filepath.Join(t.TempDir(), "a.crt"), filepath.Join(t.TempDir(), "a.key"), []string{"not a domain!"})
	if err == nil {
		t.Fatal("expected err for invalid hostname")
	}
}

func TestIssueCertKeylessCAMode(t *testing.T) {
	dir := carootSandbox(t)
	if _, err := loadCA(); err != nil {
		t.Fatal(err)
	}
	// Simulate keyless mode: only the certificate is present.
	if err := os.Remove(filepath.Join(dir, rootKeyName)); err != nil {
		t.Fatal(err)
	}
	err := IssueCert(filepath.Join(t.TempDir(), "a.crt"), filepath.Join(t.TempDir(), "a.key"), []string{"app.test"})
	if err == nil {
		t.Fatal("expected err when the CA key is missing")
	}
}

func TestIssueCertSharedOutputFile(t *testing.T) {
	carootSandbox(t)
	combined := filepath.Join(t.TempDir(), "combined.pem")
	if err := IssueCert(combined, combined, []string{"app.test"}); err != nil {
		t.Fatal(err)
	}
	rest, err := os.ReadFile(combined)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(rest)
	if block == nil {
		t.Fatal("combined file missing certificate block")
	}
}

func TestSwapEngine(t *testing.T) {
	carootSandbox(t)
	restore := SwapEngine(stubEngine{available: false})
	defer restore()
	if Available() {
		t.Error("swapped engine's Available not honored")
	}
}

type stubEngine struct {
	available bool
}

func (s stubEngine) Available() bool { return s.available }
func (stubEngine) Install() (InstallResult, error) {
	return InstallResult{}, errors.New("not implemented")
}
func (stubEngine) Uninstall() error                         { return errors.New("not implemented") }
func (stubEngine) IssueCert(string, string, []string) error { return errors.New("not implemented") }

func TestStoreEnabled(t *testing.T) {
	t.Setenv("TRUST_STORES", "")
	for _, store := range []string{"system", "nss", "java"} {
		if !storeEnabled(store) {
			t.Errorf("%s should be enabled by default", store)
		}
	}
	t.Setenv("TRUST_STORES", "system,nss")
	if !storeEnabled("system") || !storeEnabled("nss") {
		t.Error("listed stores should be enabled")
	}
	if storeEnabled("java") {
		t.Error("unlisted store should be disabled")
	}
}

func TestIsSudoDenied(t *testing.T) {
	cases := map[string]bool{
		"sudo: a password is required":               true,
		"3 incorrect authentication attempts":        true,
		"sudo: no password was provided":             false,
		"sudo-rs: authentication failed":             true,
		"tee: /etc/anchors/x.pem: Permission denied": false,
		"": false,
	}
	for out, want := range cases {
		if got := isSudoDenied([]byte(out)); got != want {
			t.Errorf("isSudoDenied(%q) = %v, want %v", out, got, want)
		}
	}
}

func readCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatal("no PEM block in certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
