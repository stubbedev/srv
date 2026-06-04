package traefik

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/validate"
)

// DNSAlias is a "type=dns" redirect entry parsed from a redirect-<name>.yml file.
// The yaml file is the canonical source of truth — there is no separate
// registry. Editing the file and regenerating dnsmasq picks up changes.
type DNSAlias struct {
	Name   string // redirect short name (filename without prefix/suffix)
	Source string // source hostname (what the client resolves)
	Target string // target hostname (resolved to an IP at dnsmasq regen time)
}

// redirectFileSchema is the on-disk schema for redirect-<name>.yml files.
// Two shapes coexist: HTTP redirects carry an `http:` block (Traefik dynamic
// config) and DNS aliases carry a `dns:` block. The two are mutually exclusive.
type redirectFileSchema struct {
	HTTP *struct{} `yaml:"http,omitempty"`
	DNS  *struct {
		Source string `yaml:"source"`
		Target string `yaml:"target"`
	} `yaml:"dns,omitempty"`
}

// ScanRedirectAliases reads every redirect-<name>.yml file in the Traefik conf
// dir, returning the subset that declare a `dns:` block. HTTP-style redirect
// files are ignored — they are handled directly by Traefik's file provider.
func ScanRedirectAliases() ([]DNSAlias, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(cfg.TraefikConfDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []DNSAlias
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, constants.RedirectConfigPrefix) || !strings.HasSuffix(name, constants.ExtYAML) {
			continue
		}
		path := filepath.Join(cfg.TraefikConfDir(), name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var parsed redirectFileSchema
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			continue
		}
		if parsed.DNS == nil {
			continue
		}
		// These files are meant to be hand-edited, so the CLI's input validation
		// no longer protects them. Re-validate here: source and target are
		// interpolated into dnsmasq `address=/<source>/<ip>` directives, so a
		// crafted hostname containing '/' or whitespace could inject extra
		// dnsmasq config lines. Skip invalid entries (with a warning) instead.
		short := strings.TrimSuffix(strings.TrimPrefix(name, constants.RedirectConfigPrefix), constants.ExtYAML)
		if err := validate.Domain(parsed.DNS.Source); err != nil {
			fmt.Fprintf(os.Stderr, "warning: redirect %q: invalid dns.source %q, skipping: %v\n", short, parsed.DNS.Source, err)
			continue
		}
		if err := validate.Domain(parsed.DNS.Target); err != nil {
			fmt.Fprintf(os.Stderr, "warning: redirect %q: invalid dns.target %q, skipping: %v\n", short, parsed.DNS.Target, err)
			continue
		}
		out = append(out, DNSAlias{
			Name:   short,
			Source: parsed.DNS.Source,
			Target: parsed.DNS.Target,
		})
	}
	return out, nil
}

// ResolvedAlias pairs a DNSAlias with the IP its target resolved to. ResolveErr
// is non-nil when target resolution failed (e.g. offline, NXDOMAIN); the caller
// decides whether to skip the entry or warn.
type ResolvedAlias struct {
	DNSAlias
	IP         string
	ResolveErr error
}

// ResolveAliases resolves each alias's target to an IPv4 address. Resolution
// uses the system resolver with a short timeout so a single unreachable target
// cannot stall the dnsmasq regen pipeline.
func ResolveAliases(aliases []DNSAlias) []ResolvedAlias {
	out := make([]ResolvedAlias, len(aliases))
	if len(aliases) == 0 {
		return out
	}

	resolver := &net.Resolver{}
	for i, a := range aliases {
		out[i].DNSAlias = a
		// 3s budget per lookup — long enough for a real resolver round trip,
		// short enough that ten stale aliases cannot block dnsmasq for half a
		// minute.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		ips, err := resolver.LookupIP(ctx, "ip4", a.Target)
		cancel()
		if err != nil {
			out[i].ResolveErr = err
			continue
		}
		if len(ips) == 0 {
			out[i].ResolveErr = fmt.Errorf("no A record for %s", a.Target)
			continue
		}
		out[i].IP = ips[0].String()
	}
	return out
}
