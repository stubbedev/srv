package site

import (
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
)

// ValidateMetadata covers the structural checks Reload depends on.
func TestValidateMetadata(t *testing.T) {
	preserveHostTrue := true
	tests := []struct {
		name    string
		meta    *SiteMetadata
		wantErr string // substring of expected error, "" means accept
	}{
		{
			name:    "nil",
			meta:    nil,
			wantErr: "metadata is nil",
		},
		{
			name:    "no domains",
			meta:    &SiteMetadata{Domains: nil},
			wantErr: "must list at least one hostname",
		},
		{
			name:    "duplicate domain",
			meta:    &SiteMetadata{Domains: []string{"a.test", "a.test"}},
			wantErr: "duplicate domain",
		},
		{
			name:    "unknown listener",
			meta:    &SiteMetadata{Domains: []string{"a.test"}, Listeners: []string{"bogus"}},
			wantErr: "unknown listener",
		},
		{
			name: "good listener",
			meta: &SiteMetadata{Domains: []string{"a.test"}, Listeners: []string{constants.ListenerInternal}},
		},
		{
			name: "route missing id",
			meta: &SiteMetadata{
				Domains: []string{"a.test"},
				Routes:  []Route{{Path: "/x", Upstream: Upstream{Kind: "localhost", Port: 1}}},
			},
			wantErr: "no id",
		},
		{
			name: "route invalid id",
			meta: &SiteMetadata{
				Domains: []string{"a.test"},
				Routes:  []Route{{ID: "BAD ID", Path: "/x", Upstream: Upstream{Kind: "localhost", Port: 1}}},
			},
			wantErr: "must match",
		},
		{
			name: "route both path and path_regex",
			meta: &SiteMetadata{
				Domains: []string{"a.test"},
				Routes:  []Route{{ID: "r", Path: "/x", PathRegex: "/y", Upstream: Upstream{Kind: "localhost", Port: 1}}},
			},
			wantErr: "exactly one",
		},
		{
			name: "rewrite without regex",
			meta: &SiteMetadata{
				Domains: []string{"a.test"},
				Routes:  []Route{{ID: "r", Path: "/x", Rewrite: "/y", Upstream: Upstream{Kind: "localhost", Port: 1}}},
			},
			wantErr: "requires `path_regex`",
		},
		{
			name: "bad regex",
			meta: &SiteMetadata{
				Domains: []string{"a.test"},
				Routes:  []Route{{ID: "r", PathRegex: "(unterminated", Upstream: Upstream{Kind: "localhost", Port: 1}}},
			},
			wantErr: "invalid path_regex",
		},
		{
			name: "unknown upstream kind",
			meta: &SiteMetadata{
				Domains: []string{"a.test"},
				Routes:  []Route{{ID: "r", Path: "/x", Upstream: Upstream{Kind: "bogus"}}},
			},
			wantErr: "upstream.kind must be one of",
		},
		{
			name: "ok with route",
			meta: &SiteMetadata{
				Domains: []string{"a.test"},
				Routes: []Route{{
					ID:           "app",
					Path:         "/app",
					Upstream:     Upstream{Kind: "localhost", Port: 6001},
					PreserveHost: &preserveHostTrue,
				}},
			},
		},
		{
			name: "fallback without url",
			meta: &SiteMetadata{
				Domains:  []string{"a.test"},
				Fallback: &Fallback{},
			},
			wantErr: "fallback.url is required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMetadata(tc.meta)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
