// Package traefik — dynamic.go defines the Traefik file-provider dynamic-config
// model shared by every writer in srv (sites, routes, proxies, redirects).
// Previously each writer re-declared its own anonymous Server/Service/Router
// structs inline; emitting through one typed model keeps the YAML shape
// consistent and means a field change happens in one place. All values are set
// programmatically (never string-interpolated into YAML), so marshalling is the
// injection-safe path for generating these files.
package traefik

import "gopkg.in/yaml.v3"

// dynServer is a single upstream URL in a load balancer.
type dynServer struct {
	URL string `yaml:"url"`
}

// dynLoadBalancer is a service's set of upstream servers.
type dynLoadBalancer struct {
	Servers          []dynServer `yaml:"servers"`
	PassHostHeader   *bool       `yaml:"passHostHeader,omitempty"`
	ServersTransport string      `yaml:"serversTransport,omitempty"` // name of a serversTransports entry
}

// dynFailover is Traefik's failover service (v3.7+): requests go to Service,
// and Errors.Status responses (or dial failures, which surface as 502) are
// transparently re-served by Fallback. Deliberately passive — no healthCheck
// block — so behaviour matches a per-request try, exactly like the retired
// nginx/daemon failover hops.
type dynFailover struct {
	Service  string             `yaml:"service"`
	Fallback string             `yaml:"fallback"`
	Errors   *dynFailoverErrors `yaml:"errors"`
}

// dynFailoverErrors selects which primary responses trigger the fallback.
type dynFailoverErrors struct {
	Status []string `yaml:"status"`
}

// dynServersTransport configures how Traefik dials an upstream. insecureSkipVerify
// lets an upstream whose certificate can't be verified (self-signed, or a cert
// whose SAN doesn't match its IP) be reached; forwardingTimeouts bounds the
// dial phase so a dead primary fails over after a bounded wait instead of the
// 30s default. Referenced by name from dynLoadBalancer.ServersTransport.
type dynServersTransport struct {
	InsecureSkipVerify bool                   `yaml:"insecureSkipVerify,omitempty"`
	ForwardingTimeouts *dynForwardingTimeouts `yaml:"forwardingTimeouts,omitempty"`
}

// dynForwardingTimeouts is the dial/first-byte timeout block of a serversTransport.
type dynForwardingTimeouts struct {
	DialTimeout string `yaml:"dialTimeout,omitempty"`
}

// dynService wraps either a load balancer or a failover under the Traefik
// `services` map. Exactly one is set: the pointer + omitempty pairing keeps a
// failover service from rendering a stray empty loadBalancer block, which
// Traefik rejects.
type dynService struct {
	LoadBalancer *dynLoadBalancer `yaml:"loadBalancer,omitempty"`
	Failover     *dynFailover     `yaml:"failover,omitempty"`
}

// dynTLS is a router's TLS block. An empty value marshals to `tls: {}` (file
// provider certs); a CertResolver routes to Let's Encrypt.
type dynTLS struct {
	CertResolver string `yaml:"certResolver,omitempty"`
}

// dynRouter is a Traefik router. Optional fields are omitempty so each writer
// only populates what it needs without leaking empty keys into the YAML.
type dynRouter struct {
	Rule        string   `yaml:"rule"`
	EntryPoints []string `yaml:"entryPoints"`
	Service     string   `yaml:"service"`
	Middlewares []string `yaml:"middlewares,omitempty"`
	Priority    int      `yaml:"priority,omitempty"`
	TLS         *dynTLS  `yaml:"tls,omitempty"`
}

// dynRedirectRegex is the redirectRegex middleware (used by HTTP redirects).
type dynRedirectRegex struct {
	Regex       string `yaml:"regex"`
	Replacement string `yaml:"replacement"`
	Permanent   bool   `yaml:"permanent"`
}

// dynReplacePathRegex is the replacePathRegex middleware (used by extra routes).
type dynReplacePathRegex struct {
	Regex       string `yaml:"regex"`
	Replacement string `yaml:"replacement"`
}

// dynMiddleware is a Traefik middleware. Exactly one field is set per instance.
type dynMiddleware struct {
	RedirectRegex    *dynRedirectRegex    `yaml:"redirectRegex,omitempty"`
	ReplacePathRegex *dynReplacePathRegex `yaml:"replacePathRegex,omitempty"`
}

// dynHTTP is the `http` block: routers, services, and optional middlewares.
type dynHTTP struct {
	Routers           map[string]dynRouter           `yaml:"routers"`
	Services          map[string]dynService          `yaml:"services"`
	Middlewares       map[string]dynMiddleware       `yaml:"middlewares,omitempty"`
	ServersTransports map[string]dynServersTransport `yaml:"serversTransports,omitempty"`
}

// DynConfig is a complete Traefik file-provider dynamic config document.
type DynConfig struct {
	HTTP dynHTTP `yaml:"http"`
}

// localTLS returns the TLS block for a local (file-provider cert) router:
// `tls: {}`. Used by sites, proxies, and redirects serving mkcert certs.
func localTLS() *dynTLS { return &dynTLS{} }

// resolverTLS returns the TLS block for a production router using the named
// ACME cert resolver (Let's Encrypt).
func resolverTLS(resolver string) *dynTLS { return &dynTLS{CertResolver: resolver} }

// MarshalDynConfig renders a DynConfig to YAML. It is the single marshalling
// entry point so callers never hand-assemble Traefik YAML as strings.
func MarshalDynConfig(c DynConfig) ([]byte, error) {
	return yaml.Marshal(&c)
}
