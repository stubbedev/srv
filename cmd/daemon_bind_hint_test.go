package cmd

import (
	"errors"
	"fmt"
	"syscall"
	"testing"

	"github.com/stubbedev/srv/internal/config"
)

func bindInUseErr() error {
	return fmt.Errorf("listen tcp 127.0.0.1:%d: bind: %w", 15353, syscall.EADDRINUSE)
}

func hintTestConfig(t *testing.T) {
	t.Helper()
	t.Setenv("SRV_ROOT", t.TempDir())
	config.ResetCache()
	t.Cleanup(config.ResetCache)
}

func TestDaemonBindHintPassthrough(t *testing.T) {
	err := errors.New("boom")
	if got := daemonBindHint(err, true); got != nil {
		t.Errorf("non-bind error got hint %v", got)
	}
	if got := daemonBindHint(bindInUseErr(), false); got != nil {
		t.Errorf("custom port conflict got hint %v", got)
	}
}

func TestDaemonBindHintWithoutDaemon(t *testing.T) {
	hintTestConfig(t)
	if got := daemonBindHint(bindInUseErr(), true); got != nil {
		t.Errorf("free lock got hint %v", got)
	}
}
