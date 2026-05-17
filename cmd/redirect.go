package cmd

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// redirect command
// =============================================================================

var redirectCmd = &cobra.Command{
	Use:   "redirect",
	Short: "Manage HTTP redirects",
	Long: `Redirect a local domain to another URL (301 permanent or 302 temporary).

Useful for mapping legacy hostnames to a new canonical URL while preserving the
request path and query string. The redirect is served with a trusted mkcert TLS
certificate so browsers do not warn before following it.`,
}

var redirectAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a redirect",
	Long: `Create an HTTP redirect from a local domain to another URL.

The incoming request path and query string are preserved and appended to the
target URL. Both http:// and https:// requests are redirected.

Examples:
  # Redirect jira.konform.com to jira.kontainer.com (301 permanent)
  srv redirect add --domain jira.konform.com --to https://jira.kontainer.com

  # Temporary (302) redirect
  srv redirect add -d old.test --to https://new.test --temporary

  # Wildcard subdomains also redirect
  srv redirect add -d legacy.test --to https://new.test --wildcard`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if redirectAddFlags.domain == "" {
			_ = cmd.Help()
			return ui.UsageError("srv redirect add --domain DOMAIN --to URL", "--domain is required (e.g. --domain old.test)")
		}
		if redirectAddFlags.to == "" {
			_ = cmd.Help()
			return ui.UsageError("srv redirect add --domain DOMAIN --to URL", "--to is required (e.g. --to https://new.example.com)")
		}
		return nil
	},
	RunE: runRedirectAdd,
}

var redirectRemoveCmd = &cobra.Command{
	Use:     "remove NAME",
	Aliases: []string{"rm"},
	Short:   "Remove a redirect",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv redirect remove NAME", "a redirect name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv redirect remove NAME", "too many arguments — expected a single redirect name, got %d", len(args))
		}
		return nil
	},
	RunE: runRedirectRemove,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return getRedirectNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

var redirectListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all redirects",
	RunE:    runRedirectList,
}

var redirectReloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Re-apply every redirect-*.yml file",
	Long: `Re-scan redirect-*.yml files and re-render the dnsmasq + Traefik dynamic
configs from them. Useful after hand-editing a redirect yaml or when a DNS-only
redirect's target IP has moved and needs to be re-resolved.`,
	RunE: runRedirectReload,
}

var redirectAddFlags struct {
	domain    string
	to        string
	name      string
	temporary bool
	permanent bool
	wildcard  bool
	force     bool
	dnsOnly   bool
}

func init() {
	redirectCmd.AddCommand(redirectAddCmd)
	redirectCmd.AddCommand(redirectRemoveCmd)
	redirectCmd.AddCommand(redirectListCmd)
	redirectCmd.AddCommand(redirectReloadCmd)

	redirectAddCmd.Flags().StringVarP(&redirectAddFlags.domain, "domain", "d", "", "Domain to redirect (e.g., old.test)")
	redirectAddCmd.Flags().StringVar(&redirectAddFlags.to, "to", "", "Target URL (e.g., https://new.example.com)")
	redirectAddCmd.Flags().StringVarP(&redirectAddFlags.name, "name", "n", "", "Redirect name (default: derived from domain)")
	redirectAddCmd.Flags().BoolVar(&redirectAddFlags.permanent, "permanent", true, "Use 301 permanent redirect (default)")
	redirectAddCmd.Flags().BoolVar(&redirectAddFlags.temporary, "temporary", false, "Use 302 temporary redirect (overrides --permanent)")
	redirectAddCmd.Flags().BoolVar(&redirectAddFlags.wildcard, "wildcard", false, "Also match one-level subdomains (e.g. *.foo.test)")
	redirectAddCmd.Flags().BoolVarP(&redirectAddFlags.force, "force", "f", false, "Overwrite existing redirect configuration")
	redirectAddCmd.Flags().BoolVar(&redirectAddFlags.dnsOnly, "dns-only", false, "Skip Traefik and TLS; emit a dnsmasq address= record so the source name resolves to the target's IP")
	_ = redirectAddCmd.MarkFlagRequired("domain")
	_ = redirectAddCmd.MarkFlagRequired("to")

	redirectCmd.GroupID = GroupProxy
	RootCmd.AddCommand(redirectCmd)
}

// =============================================================================
// Redirect Input Validation
// =============================================================================

type redirectInput struct {
	name      string
	domain    string
	to        string // target URL for HTTP mode, bare hostname for dns-only
	permanent bool
	wildcard  bool
	dnsOnly   bool
}

func validateRedirectInput() (*redirectInput, error) {
	domain := redirectAddFlags.domain
	to := strings.TrimSpace(redirectAddFlags.to)

	if err := ValidateDomain(domain); err != nil {
		return nil, fmt.Errorf("invalid domain: %w", err)
	}

	var normalizedTo string
	if redirectAddFlags.dnsOnly {
		// DNS-only redirects are an A-record swap. The target must be a bare
		// hostname — schemes, paths, and query strings have no meaning at the
		// DNS layer and would silently be ignored.
		if strings.Contains(to, "://") || strings.ContainsAny(to, "/?#") {
			return nil, fmt.Errorf("invalid --to %q: with --dns-only the target must be a bare hostname (no scheme, no path)", to)
		}
		if err := ValidateDomain(to); err != nil {
			return nil, fmt.Errorf("invalid --to hostname: %w", err)
		}
		// --wildcard, --permanent, and --temporary are HTTP-layer concepts
		// that the DNS layer cannot honor. Reject them outright so the user
		// gets a clean error instead of a silently-ignored flag.
		if redirectAddFlags.wildcard {
			return nil, fmt.Errorf("--wildcard is not supported with --dns-only (DNS records do not match wildcard children)")
		}
		if redirectAddFlags.temporary {
			return nil, fmt.Errorf("--temporary is not supported with --dns-only (DNS records carry no HTTP status code)")
		}
		normalizedTo = to
	} else {
		parsed, err := url.Parse(to)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("invalid --to URL %q: must be an absolute http:// or https:// URL", to)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("invalid --to URL %q: scheme must be http or https", to)
		}
		// Strip trailing slash on the target so path appends don't double-slash.
		normalizedTo = strings.TrimRight(to, "/")
	}

	name := redirectAddFlags.name
	if name == "" {
		name = site.SanitizeName(domain)
	}
	if err := ValidateProxyName(name); err != nil {
		return nil, fmt.Errorf("invalid redirect name: %w", err)
	}

	// --temporary overrides --permanent.
	permanent := !redirectAddFlags.temporary

	return &redirectInput{
		name:      name,
		domain:    domain,
		to:        normalizedTo,
		permanent: permanent,
		wildcard:  redirectAddFlags.wildcard,
		dnsOnly:   redirectAddFlags.dnsOnly,
	}, nil
}

// =============================================================================
// Redirect Certificate Setup
// =============================================================================

func setupRedirectCertificate(input *redirectInput) error {
	if err := traefik.CheckMkcert(); err != nil {
		return err
	}
	if !traefik.IsCAInstalled() {
		ui.Dim("Installing mkcert CA...")
		res, err := traefik.InstallCA()
		if err != nil {
			return fmt.Errorf("failed to install mkcert CA: %w", err)
		}
		reportCAInstall(res, false)
	}

	siteName := constants.RedirectConfigPrefix + input.name
	// Prefix with underscore so it sorts apart from real sites and never
	// collides with a user-named site of the same string.
	siteName = "_" + siteName

	renewed, err := traefik.EnsureLocalCert(siteName, []string{input.domain}, input.wildcard)
	if err != nil {
		return fmt.Errorf("failed to generate certificate: %w", err)
	}
	if renewed {
		ui.Dim("Generated SSL certificate for %s", input.domain)
	}
	return nil
}

func redirectSiteName(name string) string {
	return "_" + constants.RedirectConfigPrefix + name
}

// =============================================================================
// Redirect Command Handlers
// =============================================================================

func runRedirectAdd(cmd *cobra.Command, args []string) error {
	input, err := validateRedirectInput()
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	redirectFile := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+input.name+constants.ExtYAML)
	if _, err := os.Stat(redirectFile); err == nil && !redirectAddFlags.force {
		return fmt.Errorf("redirect '%s' already exists. Use --force to overwrite", input.name)
	}

	if input.dnsOnly {
		return runRedirectAddDNSOnly(cfg, input)
	}

	if err := setupRedirectCertificate(input); err != nil {
		return err
	}

	if err := traefik.RegisterLocalDomain(input.domain, input.wildcard); err != nil {
		ui.Warn("Failed to register DNS for %s: %v", input.domain, err)
	}

	if err := writeRedirectConfig(cfg, input); err != nil {
		return err
	}

	if err := traefik.UpdateDynamicConfig(); err != nil {
		ui.Warn("Failed to update Traefik config: %v", err)
	}

	code := "301"
	if !input.permanent {
		code = "302"
	}
	ui.Success("Redirect '%s' created", input.name)
	ui.Dim("https://%s -> %s (%s)", input.domain, input.to, code)
	return nil
}

// runRedirectAddDNSOnly creates a DNS-alias redirect: writes the metadata
// yaml as the source of truth and re-renders dnsmasq.conf from the scan.
func runRedirectAddDNSOnly(cfg *config.Config, input *redirectInput) error {
	if err := writeRedirectDNSConfig(cfg, input); err != nil {
		return err
	}
	// Surface upfront whether the target resolves at all — saves the user from
	// adding a typo'd target and only noticing when the redirect silently
	// doesn't work.
	resolved := traefik.ResolveAliases([]traefik.DNSAlias{{Source: input.domain, Target: input.to}})
	if len(resolved) > 0 && resolved[0].ResolveErr != nil {
		ui.Warn("Target %s could not be resolved (%v). The entry is written but dnsmasq will skip it until resolution succeeds.", input.to, resolved[0].ResolveErr)
	} else if len(resolved) > 0 {
		ui.Dim("Resolved %s -> %s", input.to, resolved[0].IP)
	}

	if err := traefik.UpdateDnsmasqConfig(); err != nil {
		ui.Warn("Failed to update dnsmasq config: %v", err)
	}

	ui.Success("Redirect '%s' created (DNS-only)", input.name)
	ui.Dim("%s -> %s (dnsmasq A-record swap, no TLS, no Traefik)", input.domain, input.to)
	return nil
}

func runRedirectRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	info := readRedirectConfig(cfg, name)

	redirectFile := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+name+constants.ExtYAML)
	if err := os.Remove(redirectFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("redirect '%s' not found", name)
		}
		return fmt.Errorf("failed to remove redirect: %w", err)
	}

	if info.DNSOnly {
		// DNS-alias redirects have no cert and no Traefik conf. Just
		// re-render dnsmasq so the entry disappears from address= lines.
		if err := traefik.UpdateDnsmasqConfig(); err != nil {
			ui.Warn("Failed to update dnsmasq config: %v", err)
		}
		ui.Success("Redirect '%s' removed", name)
		return nil
	}

	if info.Domain != "" {
		siteName := redirectSiteName(name)
		if err := traefik.RemoveLocalCerts(siteName, info.Domain); err != nil {
			ui.Warn("Failed to remove certificate: %v", err)
		}
		if err := traefik.UnregisterLocalDomain(info.Domain); err != nil {
			ui.Warn("Failed to unregister DNS for %s: %v", info.Domain, err)
		}
	}

	if err := traefik.UpdateDynamicConfig(); err != nil {
		ui.Warn("Failed to update Traefik config: %v", err)
	}

	ui.Success("Redirect '%s' removed", name)
	return nil
}

func runRedirectReload(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	names := getRedirectNames()
	if len(names) == 0 {
		ui.Dim("No redirects configured.")
		return nil
	}

	// Walk each redirect yaml so hand-edits to source/target/wildcard get
	// reflected in the DNS registry and mkcert cert set. The yaml file is
	// the source of truth — anything derived from it (certs, dnsmasq
	// records) gets rebuilt below.
	var httpCount, dnsCount int
	for _, name := range names {
		info := readRedirectConfig(cfg, name)
		if info.DNSOnly {
			dnsCount++
			continue
		}
		httpCount++
		// Ensure cert covers the (possibly edited) domain. EnsureLocalCert
		// is a no-op when the cert already covers it.
		if info.Domain != "" {
			siteName := redirectSiteName(name)
			if _, err := traefik.EnsureLocalCert(siteName, []string{info.Domain}, false); err != nil {
				ui.Warn("Failed to refresh cert for %s: %v", info.Domain, err)
			}
			if err := traefik.RegisterLocalDomain(info.Domain, false); err != nil {
				ui.Warn("Failed to register DNS for %s: %v", info.Domain, err)
			}
		}
	}

	if err := traefik.UpdateDynamicConfig(); err != nil {
		ui.Warn("Failed to update Traefik dynamic config: %v", err)
	}
	if err := traefik.UpdateDnsmasqConfig(); err != nil {
		ui.Warn("Failed to update dnsmasq config: %v", err)
	}

	ui.Success("Reloaded %d redirect(s) — %d HTTP, %d DNS-only", len(names), httpCount, dnsCount)
	return nil
}

func runRedirectList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	names := getRedirectNames()
	if len(names) == 0 {
		ui.Dim("No redirects configured. Use 'srv redirect add --domain DOMAIN --to URL' to create one.")
		return nil
	}

	traefikUp := traefik.IsRunning()

	headers := []string{"NAME", "DOMAIN", "TARGET", "MODE", "SSL", "STATUS"}
	rows := make([][]string, 0, len(names))

	for _, name := range names {
		info := readRedirectConfig(cfg, name)
		var mode, sslStatus, status string
		if info.DNSOnly {
			mode = "DNS"
			sslStatus = ui.DimText("-")
			// Status mirrors dnsmasq reachability via the local resolver
			// rather than Traefik. Keep it simple: "active" once written.
			status = "active"
		} else {
			mode = "301"
			if !info.Permanent {
				mode = "302"
			}
			sslStatus = getRedirectSSLStatus(name, info.Domain)
			status = "inactive"
			if traefikUp {
				status = "active"
			}
		}
		rows = append(rows, []string{name, info.Domain, info.Target, mode, sslStatus, ui.StatusColor(status)})
	}

	ui.PrintTable(headers, rows)
	return nil
}

// =============================================================================
// Redirect Helpers
// =============================================================================

func getRedirectSSLStatus(name, domain string) string {
	if domain == "" {
		return ui.DimText("-")
	}
	siteName := redirectSiteName(name)
	cert := traefik.GetLocalCertInfo(siteName, domain)
	if !cert.Exists {
		return ui.StatusColor("missing")
	}
	if cert.IsExpired {
		return ui.StatusColor("expired")
	}
	if cert.DaysLeft <= constants.CertExpiryWarningDays {
		return ui.StatusColor("expiring")
	}
	return ui.StatusColor("valid")
}

func getRedirectNames() []string {
	cfg, err := config.Load()
	if err != nil {
		ui.VerboseLog("Warning: could not load config: %v", err)
		return nil
	}
	entries, err := os.ReadDir(cfg.TraefikConfDir())
	if err != nil {
		ui.VerboseLog("Warning: could not read traefik conf dir: %v", err)
		return nil
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, constants.RedirectConfigPrefix) && strings.HasSuffix(name, constants.ExtYAML) {
			short := strings.TrimSuffix(strings.TrimPrefix(name, constants.RedirectConfigPrefix), constants.ExtYAML)
			names = append(names, short)
		}
	}
	return names
}

// =============================================================================
// Redirect Config File Operations
// =============================================================================

func writeRedirectConfig(cfg *config.Config, input *redirectInput) error {
	type RedirectRegex struct {
		Regex       string `yaml:"regex"`
		Replacement string `yaml:"replacement"`
		Permanent   bool   `yaml:"permanent"`
	}
	type Middleware struct {
		RedirectRegex RedirectRegex `yaml:"redirectRegex"`
	}
	type Server struct {
		URL string `yaml:"url"`
	}
	type LoadBalancer struct {
		Servers []Server `yaml:"servers"`
	}
	type Service struct {
		LoadBalancer LoadBalancer `yaml:"loadBalancer"`
	}
	type Router struct {
		Rule        string    `yaml:"rule"`
		EntryPoints []string  `yaml:"entryPoints"`
		Service     string    `yaml:"service"`
		Middlewares []string  `yaml:"middlewares"`
		TLS         *struct{} `yaml:"tls,omitempty"`
	}
	type HTTP struct {
		Routers     map[string]Router     `yaml:"routers"`
		Services    map[string]Service    `yaml:"services"`
		Middlewares map[string]Middleware `yaml:"middlewares"`
	}
	type RedirectConfig struct {
		HTTP HTTP `yaml:"http"`
	}

	routerKey := constants.RedirectConfigPrefix + input.name
	mwKey := routerKey + "-mw"
	svcKey := routerKey + "-noop"

	// Regex captures the path+query (everything after the host's leading slash)
	// so we can append it to the normalized target URL. The leading slash is
	// rebuilt in the replacement so a target without a path lands on `/<path>`.
	mw := Middleware{
		RedirectRegex: RedirectRegex{
			Regex:       `^https?://[^/]+/?(.*)$`,
			Replacement: input.to + "/$1",
			Permanent:   input.permanent,
		},
	}

	// Bind to both entrypoints so HTTP requests skip the global HTTP→HTTPS
	// detour and redirect straight to the target in one hop.
	httpsRouter := Router{
		Rule:        traefik.BuildHostRule([]string{input.domain}, input.wildcard),
		EntryPoints: []string{constants.EntryPointWebsecure},
		Service:     svcKey,
		Middlewares: []string{mwKey},
		TLS:         &struct{}{},
	}
	httpRouter := Router{
		Rule:        traefik.BuildHostRule([]string{input.domain}, input.wildcard),
		EntryPoints: []string{constants.EntryPointWeb},
		Service:     svcKey,
		Middlewares: []string{mwKey},
	}

	redirectConfig := RedirectConfig{
		HTTP: HTTP{
			Routers: map[string]Router{
				routerKey:           httpsRouter,
				routerKey + "-http": httpRouter,
			},
			// Traefik requires a service on every router even when a middleware
			// terminates the request before the service runs. Point at a black
			// hole so a misconfigured middleware fails loudly instead of
			// silently forwarding traffic somewhere.
			Services: map[string]Service{
				svcKey: {
					LoadBalancer: LoadBalancer{
						Servers: []Server{{URL: "http://127.0.0.1:1"}},
					},
				},
			},
			Middlewares: map[string]Middleware{
				mwKey: mw,
			},
		},
	}

	data, err := yaml.Marshal(&redirectConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal redirect config: %w", err)
	}

	code := "301"
	if !input.permanent {
		code = "302"
	}
	content := fmt.Sprintf("# Redirect configuration for %s - generated by srv\n# Domain: %s\n# Target: %s\n# Code:   %s\n%s",
		input.name, input.domain, input.to, code, data)

	redirectFile := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+input.name+constants.ExtYAML)
	return os.WriteFile(redirectFile, []byte(content), constants.FilePermDefault)
}

type redirectConfigInfo struct {
	Domain    string
	Target    string
	Permanent bool
	DNSOnly   bool
}

func readRedirectConfig(cfg *config.Config, name string) redirectConfigInfo {
	redirectFile := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+name+constants.ExtYAML)
	data, err := os.ReadFile(redirectFile)
	if err != nil {
		return redirectConfigInfo{Target: "unknown"}
	}

	// Use a local schema rather than reusing the proxy types because the
	// shape includes middlewares with redirectRegex.
	type redirectRegex struct {
		Regex       string `yaml:"regex"`
		Replacement string `yaml:"replacement"`
		Permanent   bool   `yaml:"permanent"`
	}
	type middleware struct {
		RedirectRegex redirectRegex `yaml:"redirectRegex"`
	}
	type router struct {
		Rule string `yaml:"rule"`
	}
	type httpConfig struct {
		Routers     map[string]router     `yaml:"routers"`
		Middlewares map[string]middleware `yaml:"middlewares"`
	}
	type dnsBlock struct {
		Source string `yaml:"source"`
		Target string `yaml:"target"`
	}
	var parsed struct {
		HTTP httpConfig `yaml:"http"`
		DNS  *dnsBlock  `yaml:"dns,omitempty"`
	}
	info := redirectConfigInfo{Target: "unknown", Permanent: true}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return info
	}
	if parsed.DNS != nil {
		info.DNSOnly = true
		info.Domain = parsed.DNS.Source
		info.Target = parsed.DNS.Target
		return info
	}
	for _, r := range parsed.HTTP.Routers {
		if domain := traefik.ExtractDomainFromRule(r.Rule); domain != "" {
			info.Domain = domain
			break
		}
	}
	for _, mw := range parsed.HTTP.Middlewares {
		if mw.RedirectRegex.Replacement != "" {
			// Trim the trailing `/$1` we appended on write.
			target := strings.TrimSuffix(mw.RedirectRegex.Replacement, "/$1")
			info.Target = target
			info.Permanent = mw.RedirectRegex.Permanent
			break
		}
	}
	return info
}

// writeRedirectDNSConfig writes a DNS-only redirect yaml. Schema is intentionally
// minimal so it reads as plain configuration rather than embedded Traefik
// internals — the yaml IS the canonical source of truth that the dnsmasq
// scanner reads back.
func writeRedirectDNSConfig(cfg *config.Config, input *redirectInput) error {
	type dnsBlock struct {
		Source string `yaml:"source"`
		Target string `yaml:"target"`
	}
	type dnsConfig struct {
		DNS dnsBlock `yaml:"dns"`
	}
	body := dnsConfig{DNS: dnsBlock{Source: input.domain, Target: input.to}}
	data, err := yaml.Marshal(&body)
	if err != nil {
		return fmt.Errorf("failed to marshal redirect config: %w", err)
	}
	content := fmt.Sprintf("# Redirect (DNS alias) for %s - generated by srv\n# Edit and run 'srv redirect reload %s' to re-resolve the target.\n%s",
		input.name, input.name, data)
	redirectFile := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+input.name+constants.ExtYAML)
	return os.WriteFile(redirectFile, []byte(content), constants.FilePermDefault)
}
