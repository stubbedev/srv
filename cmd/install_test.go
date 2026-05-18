package cmd

import (
	"testing"

	"github.com/stubbedev/srv/internal/traefik"
)

func TestResolvePortConflictsManual(t *testing.T) {
	// Unknown process → manual error.
	conflicts := []traefik.PortConflict{
		{Port: 80, Name: "HTTP", Process: ""},
	}
	if err := resolvePortConflicts(conflicts); err == nil {
		t.Error("expected err: manual conflict")
	}
}

func TestResolvePortConflictsManualNamedUnfixable(t *testing.T) {
	conflicts := []traefik.PortConflict{
		{Port: 80, Name: "HTTP", Process: "totally-foreign-process"},
	}
	if err := resolvePortConflicts(conflicts); err == nil {
		t.Error("expected err: non-autofix process")
	}
}

func TestStartSitesEmpty(t *testing.T) {
	// startSites with no sites should be a noop.
	startSites(nil)
}

