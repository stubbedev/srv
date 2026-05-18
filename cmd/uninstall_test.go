package cmd

import (
	"errors"
	"testing"

	_ "github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
)

func TestRemoveDockerNetworkOK(t *testing.T) {
	t.Cleanup(docker.SwapNewClientOK())
	if err := removeDockerNetwork("any"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRemoveDockerNetworkClientErr(t *testing.T) {
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if err := removeDockerNetwork("any"); err == nil {
		t.Error("expected err")
	}
}

// Note: runUninstall deletes the test binary (os.Executable() returns the
// running test binary path), so we don't exercise the full handler. Helper
// functions are tested above.
