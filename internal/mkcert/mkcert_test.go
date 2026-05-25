package mkcert

import (
	"errors"
	"strings"
	"testing"
)

// stubRunner is a controllable CommandRunner for tests.
type stubRunner struct {
	streamErr   error
	outOut      []byte
	outErr      error
	combinedOut []byte
	combinedErr error

	calls []string
}

func (s *stubRunner) Stream(args ...string) error {
	s.calls = append(s.calls, "Stream:"+strings.Join(args, ","))
	return s.streamErr
}
func (s *stubRunner) Output(args ...string) ([]byte, error) {
	s.calls = append(s.calls, "Output:"+strings.Join(args, ","))
	return s.outOut, s.outErr
}
func (s *stubRunner) Combined(args ...string) ([]byte, error) {
	s.calls = append(s.calls, "Combined:"+strings.Join(args, ","))
	return s.combinedOut, s.combinedErr
}

func TestParseInstallOutputCreatedCA(t *testing.T) {
	in := "Created a new local CA \\u200b💥\nUsing the local CA at \"x\"\n"
	res := parseInstallOutput(in)
	if !res.NewCA {
		t.Error("expected NewCA")
	}
	if res.RawOutput != in {
		t.Error("RawOutput should be passthrough")
	}
}

func TestParseInstallOutputSystemTrust(t *testing.T) {
	in := "The local CA is now installed in the system trust store! ⚡️"
	res := parseInstallOutput(in)
	if !res.SystemTrustOK {
		t.Error("SystemTrustOK should be true")
	}
}

func TestParseInstallOutputSystemUnsupported(t *testing.T) {
	in := "Installing to the system store is not yet supported on this Linux 😣"
	res := parseInstallOutput(in)
	if !res.SystemUnsupported {
		t.Error("SystemUnsupported should be true")
	}
}

func TestParseInstallOutputBrowserUnavailable(t *testing.T) {
	in := "Note: Firefox support is not available on your platform. ℹ️"
	res := parseInstallOutput(in)
	if !res.BrowserUnavailable {
		t.Error("BrowserUnavailable should be true")
	}
}

func TestParseInstallOutputBrowserTrustFirefox(t *testing.T) {
	in := "The local CA is now installed in the Firefox and/or Chrome/Chromium trust store"
	res := parseInstallOutput(in)
	if !res.BrowserTrustOK {
		t.Error("BrowserTrustOK should be true for Firefox line")
	}
}

func TestParseInstallOutputBrowserTrustChrome(t *testing.T) {
	in := "The local CA is now installed in the Chrome trust store (requires browser restart)"
	res := parseInstallOutput(in)
	if !res.BrowserTrustOK {
		t.Error("BrowserTrustOK should be true for Chrome line")
	}
}

func TestParseInstallOutputBrowserRestart(t *testing.T) {
	in := "Now installed in the trust store — please browser restart to take effect"
	res := parseInstallOutput(in)
	if !res.BrowserTrustOK {
		t.Error("BrowserTrustOK should be true for restart hint")
	}
}

func TestParseInstallOutputCertutilMissing(t *testing.T) {
	cases := []string{
		`Warning: no "certutil" tool installed.`,
		`warning: "certutil" is not available; install nss-tools`,
	}
	for _, in := range cases {
		res := parseInstallOutput(in)
		if !res.CertutilMissing {
			t.Errorf("CertutilMissing should be true for %q", in)
		}
	}
}

func TestParseInstallOutputSudoDenied(t *testing.T) {
	cases := []string{
		`sudo: Authentication failed, try again.`,
		`sudo-rs: 3 incorrect authentication attempts`,
		`sudo: a password is required`,
	}
	for _, in := range cases {
		res := parseInstallOutput(in)
		if !res.SudoDenied {
			t.Errorf("SudoDenied should be true for %q", in)
		}
	}
}

func TestParseInstallOutputCombination(t *testing.T) {
	in := strings.Join([]string{
		"Created a new local CA at \"~/.mkcert\"",
		"The local CA is now installed in the system trust store!",
		"The local CA is now installed in the Firefox trust store!",
	}, "\n")
	res := parseInstallOutput(in)
	if !res.NewCA || !res.SystemTrustOK || !res.BrowserTrustOK {
		t.Errorf("combined parse failed: %+v", res)
	}
}

func TestParseInstallOutputEmpty(t *testing.T) {
	res := parseInstallOutput("")
	if res.NewCA || res.SystemTrustOK || res.BrowserTrustOK ||
		res.SystemUnsupported || res.BrowserUnavailable || res.CertutilMissing {
		t.Errorf("empty input should yield zero result, got %+v", res)
	}
	if res.RawOutput != "" {
		t.Errorf("RawOutput = %q, want empty", res.RawOutput)
	}
}

func TestAvailableTrueWhenLookPathSucceeds(t *testing.T) {
	t.Cleanup(SwapLookPath(func(string) (string, error) { return "/fake/bin/mkcert", nil }))
	if !Available() {
		t.Error("Available() should be true when lookPath returns a hit")
	}
}

func TestAvailableFalseWhenLookPathFails(t *testing.T) {
	t.Cleanup(SwapLookPath(func(string) (string, error) { return "", errors.New("not found") }))
	if Available() {
		t.Error("Available() should be false when lookPath returns an error")
	}
}

func TestErrNotInstalledMessage(t *testing.T) {
	if ErrNotInstalled == nil {
		t.Fatal("ErrNotInstalled should be non-nil")
	}
	if !strings.Contains(ErrNotInstalled.Error(), "mkcert") {
		t.Errorf("err msg = %q", ErrNotInstalled.Error())
	}
}

func TestSwapRunnerRestores(t *testing.T) {
	prev := Runner
	stub := &stubRunner{}
	restore := SwapRunner(stub)
	if Runner != stub {
		t.Fatal("SwapRunner did not install stub")
	}
	restore()
	if Runner != prev {
		t.Errorf("restore did not revert")
	}
}

func TestRunDelegates(t *testing.T) {
	stub := &stubRunner{streamErr: errors.New("boom")}
	t.Cleanup(SwapRunner(stub))
	if err := Run("-foo", "x"); err == nil || err.Error() != "boom" {
		t.Errorf("Run err = %v", err)
	}
	if len(stub.calls) != 1 || stub.calls[0] != "Stream:-foo,x" {
		t.Errorf("calls = %v", stub.calls)
	}
}

func TestRunQuietDelegates(t *testing.T) {
	stub := &stubRunner{outOut: []byte("ignored"), outErr: nil}
	t.Cleanup(SwapRunner(stub))
	if err := RunQuiet("-cert"); err != nil {
		t.Errorf("err: %v", err)
	}
	if len(stub.calls) != 1 || stub.calls[0] != "Output:-cert" {
		t.Errorf("calls = %v", stub.calls)
	}
}

func TestRunQuietForwardsErr(t *testing.T) {
	stub := &stubRunner{outErr: errors.New("exit 1")}
	t.Cleanup(SwapRunner(stub))
	if err := RunQuiet(); err == nil {
		t.Error("expected error")
	}
}

func TestOutputDelegates(t *testing.T) {
	stub := &stubRunner{outOut: []byte("/root/ca"), outErr: nil}
	t.Cleanup(SwapRunner(stub))
	got, err := Output("-CAROOT")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "/root/ca" {
		t.Errorf("got %q", got)
	}
}

func TestInstallParsesAndAttachesCARootPath(t *testing.T) {
	stub := &stubRunner{
		combinedOut: []byte("Created a new local CA at \"x\"\nThe local CA is now installed in the system trust store!"),
		outOut:      []byte("  /etc/mkcert  \n"),
	}
	t.Cleanup(SwapRunner(stub))

	res, err := Install()
	if err != nil {
		t.Errorf("Install err: %v", err)
	}
	if !res.NewCA || !res.SystemTrustOK {
		t.Errorf("parse incorrect: %+v", res)
	}
	if res.CARootPath == "" {
		t.Error("CARootPath empty")
	}
	if !strings.HasSuffix(res.CARootPath, "rootCA.pem") {
		t.Errorf("CARootPath = %q", res.CARootPath)
	}
}

func TestInstallReturnsRunErr(t *testing.T) {
	stub := &stubRunner{combinedErr: errors.New("exit 2")}
	t.Cleanup(SwapRunner(stub))
	_, err := Install()
	if err == nil {
		t.Error("expected err")
	}
}

func TestInstallSwallowsCARootError(t *testing.T) {
	stub := &stubRunner{
		combinedOut: []byte("Created a new local CA"),
		outErr:      errors.New("caroot fail"),
	}
	t.Cleanup(SwapRunner(stub))
	res, err := Install()
	if err != nil {
		t.Errorf("Install err: %v", err)
	}
	if res.CARootPath != "" {
		t.Errorf("CARootPath should be empty when caroot fails: %q", res.CARootPath)
	}
	if !res.NewCA {
		t.Error("NewCA should still parse")
	}
}

func TestDefaultRunnerErrorsWhenMkcertMissing(t *testing.T) {
	t.Cleanup(SwapLookPath(func(string) (string, error) { return "", errors.New("not found") }))
	if err := (defaultRunner{}.Stream("--help")); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("Stream err = %v, want ErrNotInstalled", err)
	}
	if _, err := (defaultRunner{}.Output("-CAROOT")); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("Output err = %v, want ErrNotInstalled", err)
	}
	if _, err := (defaultRunner{}.Combined("-CAROOT")); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("Combined err = %v, want ErrNotInstalled", err)
	}
}
