// Package redirect holds the schema-bearing types for srv-managed redirect
// configuration files. Only the DNS-only variant is modelled here — the
// HTTP redirect file uses Traefik's own dynamic-config shape, which is
// owned upstream and not something we want to mirror as a srv schema.
package redirect

// DNSBlock is the source → target pair persisted in a DNS-only redirect.
// `source` is the hostname dnsmasq pins; `target` is a bare hostname
// resolved at write time + on `srv redirect reload`.
type DNSBlock struct {
	// Hostname dnsmasq pins (the user-visible source domain).
	Source string `yaml:"source"`
	// Bare hostname the source resolves to; re-resolved on `srv redirect reload`.
	Target string `yaml:"target"`
}

// DNSOnlyConfig is the on-disk shape of a `redirect-<name>.yml` file when
// the redirect is DNS-only (created via `srv redirect add --dns-only`).
// HTTP redirects use Traefik's dynamic-config shape under `http:` and are
// intentionally not represented here.
type DNSOnlyConfig struct {
	// source → target hostname pair for the DNS-layer redirect.
	DNS DNSBlock `yaml:"dns"`
}
