//go:build linux

package mkcert

import (
	"testing"
)

func TestInstallUnsupportedSystemStore(t *testing.T) {
	carootSandbox(t)
	t.Setenv("TRUST_STORES", "system")

	restore := SwapSystemTrustCommand(func() (string, []string) { return "", nil })
	defer restore()

	res, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	if !res.SystemUnsupported {
		t.Error("SystemUnsupported not reported for a missing store")
	}
	if res.SystemTrustOK {
		t.Error("SystemTrustOK must stay false when nothing was installed")
	}
	if res.CARootPath == "" {
		t.Error("CARootPath missing")
	}
	if !res.NewCA {
		t.Error("a fresh CA should have been created")
	}
	if res.RawOutput == "" {
		t.Error("RawOutput should carry the engine log for debugging")
	}
}
