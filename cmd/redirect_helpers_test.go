package cmd

import (
	"testing"

	"github.com/stubbedev/srv/internal/constants"
)

func TestRedirectSiteName(t *testing.T) {
	got := redirectSiteName("foo")
	want := "_" + constants.RedirectConfigPrefix + "foo"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
