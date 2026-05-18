package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/mkcert"
)

func TestRunAddDockerDown(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if err := runAdd(nil, []string{"/tmp"}); err == nil {
		t.Error("expected err: docker down")
	}
}

func TestRunAddNotInitialized(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	if err := runAdd(nil, []string{"/tmp"}); err == nil {
		t.Error("expected err: not initialized")
	}
}

func TestRunAddStaticHappy(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "blog")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := mustLoadConfig(t)
	t.Cleanup(docker.SwapNewClientWithNetwork(cfg.NetworkName))
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))

	resetAddFlags()
	addFlags.domain = "blog.local"
	addFlags.name = "blog"
	addFlags.local = true
	addFlags.typeOverride = "static"
	defer resetAddFlags()

	if err := runAdd(nil, []string{projectDir}); err != nil {
		t.Errorf("err: %v", err)
	}
}
