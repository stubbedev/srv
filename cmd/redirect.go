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
	"github.com/stubbedev/srv/internal/redirect"
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

// setupRedirectCertificate ensures mkcert is installed and a cert exists for
// the redirect's source domain. Delegates to the shared helper used by `srv
// proxy`.
func setupRedirectCertificate(input *redirectInput) error {
	return ensureLocalCertForResource(redirectSiteName(input.name), input.domain, input.wildcard)
}

// redirectSiteName is the synthetic site name under which a redirect's local
// cert is stored. Prefixed with underscore so it sorts apart from real sites
// and never collides with a user-named site of the same string.
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

	// Orchestration (validation, cert, DNS, config write) lives in
	// internal/redirect so the CLI and the MCP add_redirect tool share it.
	res, err := redirect.Add(cfg, redirect.AddSpec{
		Name:      input.name,
		Domain:    input.domain,
		To:        input.to,
		Permanent: input.permanent,
		Wildcard:  input.wildcard,
		DNSOnly:   input.dnsOnly,
		Force:     redirectAddFlags.force,
	})
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		ui.Warn("%s", w)
	}
	if res.DNSOnly {
		ui.Success("Redirect '%s' created (DNS-only)", res.Name)
		ui.Dim("%s -> %s (dnsmasq A-record swap, no TLS, no Traefik)", res.Domain, res.Target)
		return nil
	}
	code := "301"
	if !input.permanent {
		code = "302"
	}
	ui.Success("Redirect '%s' created", res.Name)
	ui.Dim("https://%s -> %s (%s)", res.Domain, res.Target, code)
	return nil
}

func runRedirectRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	warnings, err := redirect.RemoveRedirect(cfg, name)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		ui.Warn("%s", w)
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

// redirectListRow is the json shape under `srv redirect list --format json`.
type redirectListRow struct {
	Name    string `json:"name"`
	Domain  string `json:"domain"`
	Target  string `json:"target"`
	Mode    string `json:"mode"`
	DNSOnly bool   `json:"dns_only"`
	SSL     string `json:"ssl,omitempty"`
	Status  string `json:"status"`
}

func runRedirectList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	names := getRedirectNames()
	if len(names) == 0 {
		if jsonOutput() {
			return ui.PrintJSON([]redirectListRow{})
		}
		ui.Dim("No redirects configured. Use 'srv redirect add --domain DOMAIN --to URL' to create one.")
		return nil
	}

	traefikUp := traefik.IsRunning()

	if jsonOutput() {
		out := make([]redirectListRow, 0, len(names))
		for _, name := range names {
			info := readRedirectConfig(cfg, name)
			row := redirectListRow{
				Name:    name,
				Domain:  info.Domain,
				Target:  info.Target,
				DNSOnly: info.DNSOnly,
			}
			if info.DNSOnly {
				row.Mode = "dns"
				row.Status = "active"
			} else {
				row.Mode = "301"
				if !info.Permanent {
					row.Mode = "302"
				}
				row.SSL = plainRedirectSSLStatus(name, info.Domain)
				row.Status = "inactive"
				if traefikUp {
					row.Status = "active"
				}
			}
			out = append(out, row)
		}
		return ui.PrintJSON(out)
	}

	headers := []string{"NAME", "DOMAIN", "TARGET", "MODE", "SSL", "STATUS"}
	rows := make([][]string, 0, len(names))
	for _, name := range names {
		info := readRedirectConfig(cfg, name)
		var mode, sslStatus, status string
		if info.DNSOnly {
			mode = "DNS"
			sslStatus = ui.DimText("-")
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

// plainRedirectSSLStatus mirrors getRedirectSSLStatus without colour for json.
func plainRedirectSSLStatus(name, domain string) string {
	return localCertStatus(redirectSiteName(name), domain)
}

// =============================================================================
// Redirect Helpers
// =============================================================================

func getRedirectSSLStatus(name, domain string) string {
	return localCertStatusColored(redirectSiteName(name), domain)
}

func getRedirectNames() []string {
	return scanConfigNames(constants.RedirectConfigPrefix)
}

// =============================================================================
// Redirect Config File Operations
// =============================================================================

// writeRedirectConfig renders the HTTP redirect's Traefik file config. The
// rendering lives in internal/traefik (shared with the other dynamic-config
// writers); this wrapper just builds the input struct.
func writeRedirectConfig(cfg *config.Config, input *redirectInput) error {
	return traefik.WriteRedirectConfig(cfg, traefik.HTTPRedirect{
		Name:      input.name,
		Domain:    input.domain,
		To:        input.to,
		Permanent: input.permanent,
		Wildcard:  input.wildcard,
	})
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

// writeRedirectDNSConfig writes a DNS-only redirect yaml. The rendering lives in
// internal/redirect (shared with the MCP add_redirect tool); this wrapper keeps
// the cmd-side call site and tests stable.
func writeRedirectDNSConfig(cfg *config.Config, input *redirectInput) error {
	return redirect.WriteDNSConfig(cfg, input.name, input.domain, input.to)
}
