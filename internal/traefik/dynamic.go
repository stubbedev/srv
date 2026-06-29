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

// dynServersTransport configures how Traefik dials an HTTPS upstream. Only the
// insecureSkipVerify knob is modelled — it lets an upstream whose certificate
// can't be verified (self-signed, or a cert whose SAN doesn't match its IP) be
// reached. Referenced by name from dynLoadBalancer.ServersTransport.
type dynServersTransport struct {
	InsecureSkipVerify bool `yaml:"insecureSkipVerify"`
}

// dynService wraps a load balancer under the Traefik `services` map.
type dynService struct {
	LoadBalancer dynLoadBalancer `yaml:"loadBalancer"`
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
