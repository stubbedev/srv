// Package redirect — orchestrate.go holds the headless add/remove/reload flow
// shared by the `srv redirect` CLI and the MCP redirect tools. Both surfaces
// validate, issue certs, register DNS, and render the same yaml through here so
// they cannot drift.
package redirect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/fsutil"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/validate"
)

// certSiteName is the synthetic site name a redirect's local cert is stored
// under (mirrors the CLI's redirectSiteName).
func certSiteName(name string) string { return "_" + constants.RedirectConfigPrefix + name }

// AddSpec describes a redirect to create.
type AddSpec struct {
	Name      string // optional; derived from Domain when empty
	Domain    string
	To        string // target URL (HTTP) or bare hostname (DNS-only)
	Permanent bool   // 301 vs 302 (HTTP only)
	Wildcard  bool   // HTTP only
	DNSOnly   bool
	Force     bool
}

// AddResult reports what Add produced.
type AddResult struct {
	Name     string   `json:"name"`
	Domain   string   `json:"domain"`
	Target   string   `json:"target"`
	DNSOnly  bool     `json:"dns_only"`
	Warnings []string `json:"warnings,omitempty"`
}

// Add validates the spec and creates either an HTTP redirect (Traefik 301/302
// with a local cert) or a DNS-only alias (dnsmasq A-record swap).
func Add(cfg *config.Config, spec AddSpec) (*AddResult, error) {
	name, normalizedTo, err := validateAddSpec(spec)
	if err != nil {
		return nil, err
	}

	redirectFile := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+name+constants.ExtYAML)
	if !spec.Force {
		if _, statErr := os.Stat(redirectFile); statErr == nil {
			return nil, fmt.Errorf("redirect %q already exists (set force to overwrite)", name)
		}
	}

	res := &AddResult{Name: name, Domain: spec.Domain, Target: normalizedTo, DNSOnly: spec.DNSOnly}

	if spec.DNSOnly {
		if err := WriteDNSConfig(cfg, name, spec.Domain, normalizedTo); err != nil {
			return nil, err
		}
		resolved := traefik.ResolveAliases([]traefik.DNSAlias{{Source: spec.Domain, Target: normalizedTo}})
		if len(resolved) > 0 && resolved[0].ResolveErr != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("target %s did not resolve (%v); dnsmasq will skip it until it does", normalizedTo, resolved[0].ResolveErr))
		}
		if err := traefik.UpdateDnsmasqConfig(); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("update dnsmasq config: %v", err))
		}
		return res, nil
	}

	if _, err := traefik.EnsureResourceCert(certSiteName(name), spec.Domain, spec.Wildcard); err != nil {
		return nil, err
	}
	if err := traefik.RegisterLocalDomain(spec.Domain, spec.Wildcard); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("register DNS for %s: %v", spec.Domain, err))
	}
	if err := traefik.WriteRedirectConfig(cfg, traefik.HTTPRedirect{
		Name:      name,
		Domain:    spec.Domain,
		To:        normalizedTo,
		Permanent: spec.Permanent,
		Wildcard:  spec.Wildcard,
	}); err != nil {
		return nil, err
	}
	if err := traefik.UpdateDynamicConfig(); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("update Traefik config: %v", err))
	}
	return res, nil
}

// RemoveRedirect deletes a redirect's yaml and the derived cert/DNS state, then
// refreshes whichever layer it lived in. Errors only when the redirect is
// missing; per-step failures are returned as warnings.
func RemoveRedirect(cfg *config.Config, name string) (warnings []string, err error) {
	info := ReadInfo(cfg, name)
	redirectFile := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+name+constants.ExtYAML)
	if rmErr := os.Remove(redirectFile); rmErr != nil {
		if os.IsNotExist(rmErr) {
			return nil, fmt.Errorf("redirect %q not found", name)
		}
		return nil, fmt.Errorf("remove redirect: %w", rmErr)
	}

	if info.DNSOnly {
		if err := traefik.UpdateDnsmasqConfig(); err != nil {
			warnings = append(warnings, fmt.Sprintf("update dnsmasq config: %v", err))
		}
		return warnings, nil
	}

	if info.Domain != "" {
		if err := traefik.RemoveLocalCerts(certSiteName(name), info.Domain); err != nil {
			warnings = append(warnings, fmt.Sprintf("remove certificate: %v", err))
		}
		if err := traefik.UnregisterLocalDomain(info.Domain); err != nil {
			warnings = append(warnings, fmt.Sprintf("unregister DNS for %s: %v", info.Domain, err))
		}
	}
	if err := traefik.UpdateDynamicConfig(); err != nil {
		warnings = append(warnings, fmt.Sprintf("update Traefik config: %v", err))
	}
	return warnings, nil
}

// WriteDNSConfig writes a DNS-only redirect yaml (the canonical source the
// dnsmasq scanner reads back). Moved here from cmd so the producer lives beside
// the DNSOnlyConfig schema it emits.
func WriteDNSConfig(cfg *config.Config, name, source, target string) error {
	body := DNSOnlyConfig{DNS: DNSBlock{Source: source, Target: target}}
	data, err := yaml.Marshal(&body)
	if err != nil {
		return fmt.Errorf("marshal redirect config: %w", err)
	}
	content := fmt.Sprintf("# yaml-language-server: $schema=%s\n# Redirect (DNS alias) for %s - generated by srv\n# Edit and run 'srv redirect reload %s' to re-resolve the target.\n%s",
		constants.RedirectDNSSchemaURL, name, name, data)
	redirectFile := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+name+constants.ExtYAML)
	return fsutil.AtomicWriteFile(redirectFile, []byte(content), constants.FilePermDefault)
}

// Info is the subset of a redirect file needed to remove or reload it.
type Info struct {
	Domain  string
	DNSOnly bool
}

// ReadInfo parses redirect-<name>.yml enough to drive remove/reload: whether it
// is a DNS-only alias and the source domain.
func ReadInfo(cfg *config.Config, name string) Info {
	path := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+name+constants.ExtYAML)
	data, err := os.ReadFile(path)
	if err != nil {
		return Info{}
	}
	var parsed struct {
		HTTP struct {
			Routers map[string]struct {
				Rule string `yaml:"rule"`
			} `yaml:"routers"`
		} `yaml:"http"`
		DNS *DNSBlock `yaml:"dns,omitempty"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return Info{}
	}
	if parsed.DNS != nil {
		return Info{Domain: parsed.DNS.Source, DNSOnly: true}
	}
	for _, r := range parsed.HTTP.Routers {
		if d := traefik.ExtractDomainFromRule(r.Rule); d != "" {
			return Info{Domain: d}
		}
	}
	return Info{}
}

// validateAddSpec mirrors the CLI's validateRedirectInput.
func validateAddSpec(spec AddSpec) (name, normalizedTo string, err error) {
	if err := validate.Domain(spec.Domain); err != nil {
		return "", "", fmt.Errorf("invalid domain: %w", err)
	}
	to := strings.TrimSpace(spec.To)
	if spec.DNSOnly {
		if strings.Contains(to, "://") || strings.ContainsAny(to, "/?#") {
			return "", "", fmt.Errorf("invalid target %q: with dns_only the target must be a bare hostname (no scheme, no path)", to)
		}
		if err := validate.Domain(to); err != nil {
			return "", "", fmt.Errorf("invalid target hostname: %w", err)
		}
		if spec.Wildcard {
			return "", "", fmt.Errorf("wildcard is not supported with dns_only")
		}
		normalizedTo = to
	} else {
		if !strings.HasPrefix(to, "http://") && !strings.HasPrefix(to, "https://") {
			return "", "", fmt.Errorf("invalid target %q: must be an absolute http:// or https:// URL", to)
		}
		normalizedTo = strings.TrimRight(to, "/")
	}
	name = spec.Name
	if name == "" {
		name = site.SanitizeName(spec.Domain)
	}
	if err := validate.ProxyName(name); err != nil {
		return "", "", fmt.Errorf("invalid redirect name: %w", err)
	}
	return name, normalizedTo, nil
}
