// Package valet parses Laravel Valet's nginx configuration files into srv-shaped
// site specifications. The parser is intentionally narrow: it recognises only
// the directive set Valet itself emits, not arbitrary nginx grammar.
package valet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Config mirrors the subset of valet's config.json that the importer uses.
type Config struct {
	Domain string   `json:"domain"`
	Paths  []string `json:"paths"`
	Port   string   `json:"port"`
}

// ReadConfig reads valet's config.json from the given valet dir; returns a
// zero-value Config if the file is absent or unreadable.
func ReadConfig(valetDir string) Config {
	b, err := os.ReadFile(filepath.Join(valetDir, "config.json"))
	if err != nil {
		return Config{}
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}
	}
	return c
}

// Site is the imported description of one Valet nginx file.
type Site struct {
	File         string   // Source nginx config path
	Domain       string   // Canonical hostname (first server_name entry, stripped of leading "www.")
	Aliases      []string // Additional hostnames from the same server_name list
	Wildcard     bool     // True if any "*.<domain>" pattern appears
	IsPHP        bool     // FastCGI block present
	ProjectPath  string   // Resolved project directory (PHP sites only)
	Internal     bool     // True if a parallel `listen 88` server block exists
	MaxBody      string   // From client_max_body_size
	ReadTimeout  string   // From fastcgi_read_timeout
	SendTimeout  string   // From fastcgi_send_timeout
	ConnTimeout  string   // From fastcgi_connect_timeout
	ProxyTarget  string   // For proxy-mode sites: "localhost:N" (no scheme)
	FallbackURL  string   // From a @fallback location's proxy_pass (if any)
	Routes       []Route  // Extra location blocks attached to the site
	UnknownNotes []string // Anything the parser saw but couldn't translate
}

// Route is an extra Traefik router derived from a non-root location block.
type Route struct {
	ID        string
	Path      string
	PathRegex string
	Rewrite   string
	Port      int
}

// ParseDir walks the supplied Valet Nginx directory (typically
// ~/.valet/Nginx) and returns one Site per recognised config file.
// Files prefixed with `_` and `.keep` markers are skipped.
//
// parkedPaths and sitesDir together drive project-path resolution: for each
// host, the resolver first tries every hyphen-split candidate against the
// Sites/ symlink table, then falls back to looking the same candidates up
// inside every parked path (valet's automatic directory-name mode).
func ParseDir(nginxDir, sitesDir string, parkedPaths []string) ([]*Site, error) {
	entries, err := os.ReadDir(nginxDir)
	if err != nil {
		return nil, err
	}
	var sites []*Site
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(nginxDir, name)
		site, perr := ParseFile(path, sitesDir, parkedPaths)
		if perr != nil {
			site = &Site{File: path, UnknownNotes: []string{perr.Error()}}
		}
		sites = append(sites, site)
	}
	return sites, nil
}

// ParseFile parses a single Valet Nginx config file.
func ParseFile(path, sitesDir string, parkedPaths []string) (*Site, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	site := &Site{File: path}
	blocks := splitServerBlocks(string(data))

	// First pass — note any `listen 88` block for internal-http detection.
	for _, b := range blocks {
		if matchListen.MatchString(b) && strings.Contains(b, "listen 88") {
			site.Internal = true
		}
	}

	// Pick the HTTPS server block (listen 443) as the canonical routing
	// definition. Fall back to listen 80 if no SSL block exists.
	var primary string
	for _, b := range blocks {
		if listen443.MatchString(b) {
			primary = b
			break
		}
	}
	if primary == "" {
		for _, b := range blocks {
			if listen80.MatchString(b) {
				primary = b
				break
			}
		}
	}
	if primary == "" {
		return site, fmt.Errorf("no recognisable server block in %s", path)
	}

	parsePrimaryBlock(primary, site)

	if site.IsPHP {
		site.ProjectPath = resolveValetProjectPath(site.Domain, sitesDir, parkedPaths)
	}
	return site, nil
}

// parsePrimaryBlock extracts directives and named locations from the HTTPS
// server block, populating the Site struct in place.
func parsePrimaryBlock(block string, site *Site) {
	if m := serverNameRe.FindStringSubmatch(block); len(m) == 2 {
		hosts := strings.Fields(m[1])
		dedup := make(map[string]bool)
		for _, h := range hosts {
			h = strings.TrimSpace(strings.TrimSuffix(h, ";"))
			if h == "" {
				continue
			}
			if strings.HasPrefix(h, "*.") {
				site.Wildcard = true
				continue
			}
			if strings.HasPrefix(h, "www.") {
				// www aliases are ignored; valet always pairs them with the apex.
				continue
			}
			if dedup[h] {
				continue
			}
			dedup[h] = true
			if site.Domain == "" {
				site.Domain = h
			} else {
				site.Aliases = append(site.Aliases, h)
			}
		}
	}

	if strings.Contains(block, "fastcgi_pass") {
		site.IsPHP = true
	}

	if m := clientMaxBodyRe.FindStringSubmatch(block); len(m) == 2 {
		site.MaxBody = m[1]
	}
	if m := fastcgiReadTimeoutRe.FindStringSubmatch(block); len(m) == 2 {
		site.ReadTimeout = m[1]
	}
	if m := fastcgiSendTimeoutRe.FindStringSubmatch(block); len(m) == 2 {
		site.SendTimeout = m[1]
	}
	if m := fastcgiConnectTimeoutRe.FindStringSubmatch(block); len(m) == 2 {
		site.ConnTimeout = m[1]
	}

	// Named locations: `location @name { proxy_pass URL; ... }`. Used by
	// valet's prod-fallback pattern (error_page 5xx = @fallback). nginx
	// `set $var "value";` directives inside the same location are resolved
	// before extracting the URL so https://$prod_upstream collapses to the
	// literal hostname.
	named := namedLocationRe.FindAllStringSubmatch(block, -1)
	fallbacks := make(map[string]string)
	for _, m := range named {
		if len(m) >= 3 {
			body := m[2]
			vars := extractNginxSetVars(body)
			if pp := proxyPassRe.FindStringSubmatch(body); len(pp) == 2 {
				fallbacks[m[1]] = expandNginxVars(strings.TrimSuffix(pp[1], ";"), vars)
			}
		}
	}
	if epm := errorPageRe.FindStringSubmatch(block); len(epm) >= 2 {
		if url, ok := fallbacks[epm[1]]; ok {
			site.FallbackURL = url
		}
	}

	// Path-style locations: `location /foo { proxy_pass http://...:PORT; }`.
	// Skips the catch-all `location /` and the FastCGI block (regex matchers).
	plain := plainLocationRe.FindAllStringSubmatch(block, -1)
	for _, m := range plain {
		if len(m) < 3 {
			continue
		}
		path := strings.TrimSpace(m[1])
		body := m[2]
		if path == "/" {
			// Root location: detect proxy mode.
			if pp := proxyPassRe.FindStringSubmatch(body); len(pp) == 2 {
				site.ProxyTarget = stripScheme(strings.TrimSuffix(pp[1], ";"))
			}
			continue
		}
		if pp := proxyPassRe.FindStringSubmatch(body); len(pp) == 2 {
			port := extractPort(pp[1])
			if port == 0 {
				continue
			}
			site.Routes = append(site.Routes, Route{
				ID:   slugify(path),
				Path: path,
				Port: port,
			})
		}
	}

	// Regex locations: `location ~ ^/regex$ { rewrite ... break; proxy_pass http://upstream; }`.
	regexLocs := regexLocationRe.FindAllStringSubmatch(block, -1)
	for _, m := range regexLocs {
		if len(m) < 3 {
			continue
		}
		pattern := m[1]
		body := m[2]
		rw := rewriteRe.FindStringSubmatch(body)
		pp := proxyPassRe.FindStringSubmatch(body)
		if len(pp) < 2 {
			continue
		}
		port := extractPort(pp[1])
		if port == 0 {
			// Upstream name (named upstream block); not enough info to convert.
			site.UnknownNotes = append(site.UnknownNotes, fmt.Sprintf("regex location %q → named upstream %q (translate manually)", pattern, pp[1]))
			continue
		}
		route := Route{
			ID:        slugify(pattern),
			PathRegex: pattern,
			Port:      port,
		}
		if len(rw) >= 3 {
			route.Rewrite = rw[2]
		}
		site.Routes = append(site.Routes, route)
	}
}

// resolveValetProjectPath turns a domain like "cms-kontainer.test" into the
// project directory. Valet maps hosts onto projects two ways:
//
//  1. Linked sites: ~/.valet/Sites/<name> is a symlink to the project dir.
//     Subdomain stripping lets `<prefix>-<name>.test` and `<name>-<suffix>.test`
//     resolve via Sites/<name> too.
//  2. Parked sites: any directory under one of the paths in config.json/paths
//     is reachable as `<dirname>.test` automatically.
//
// We try every hyphen-split substring of the first label against both the
// Sites symlink table and each parked path. Returns "" when no candidate
// resolves.
func resolveValetProjectPath(domain, sitesDir string, parkedPaths []string) string {
	base := domain
	if idx := strings.Index(domain, "."); idx >= 0 {
		base = domain[:idx]
	}

	// Generate all candidates: full label, then each hyphen-delimited
	// suffix (cms-kontainer → kontainer) and prefix (kontainer-8080 →
	// kontainer), then each single segment.
	seen := map[string]bool{}
	var candidates []string
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		candidates = append(candidates, s)
	}
	add(base)
	parts := strings.Split(base, "-")
	// Successive suffixes: drop one prefix segment at a time.
	for i := 1; i < len(parts); i++ {
		add(strings.Join(parts[i:], "-"))
	}
	// Successive prefixes: drop one suffix segment at a time.
	for i := len(parts) - 1; i >= 1; i-- {
		add(strings.Join(parts[:i], "-"))
	}
	// Individual segments.
	for _, p := range parts {
		add(p)
	}

	// Sites/ symlink lookup wins.
	if sitesDir != "" {
		for _, c := range candidates {
			p := filepath.Join(sitesDir, c)
			if resolved, err := filepath.EvalSymlinks(p); err == nil {
				return resolved
			}
		}
	}
	// Parked-path discovery: any directory inside a parked path that matches
	// a candidate name is a valid project (valet's directory-name mode).
	for _, parked := range parkedPaths {
		if parked == "" || parked == sitesDir {
			continue
		}
		for _, c := range candidates {
			p := filepath.Join(parked, c)
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				return p
			}
		}
	}
	return ""
}

// splitServerBlocks returns each top-level `server { … }` block as a string.
// Tracks brace depth so nested braces (inside `location` blocks) don't close
// the server block prematurely.
func splitServerBlocks(src string) []string {
	var blocks []string
	for {
		idx := strings.Index(src, "server")
		if idx < 0 {
			break
		}
		// Locate the opening `{` after `server`.
		brace := strings.Index(src[idx:], "{")
		if brace < 0 {
			break
		}
		start := idx + brace
		depth := 0
		end := -1
		for i := start; i < len(src); i++ {
			switch src[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end != -1 {
				break
			}
		}
		if end < 0 {
			break
		}
		blocks = append(blocks, src[idx:end+1])
		src = src[end+1:]
	}
	return blocks
}

// extractPort returns the port number from a proxy_pass target like
// "http://localhost:6001" or "http://127.0.0.1:8000/path". Returns 0 when
// the target uses a named upstream or omits a port.
func extractPort(target string) int {
	target = strings.TrimSuffix(target, ";")
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "http://")
	target = strings.TrimPrefix(target, "https://")
	if i := strings.IndexAny(target, "/?"); i >= 0 {
		target = target[:i]
	}
	if i := strings.LastIndex(target, ":"); i >= 0 {
		var port int
		_, _ = fmt.Sscanf(target[i+1:], "%d", &port)
		return port
	}
	return 0
}

// stripScheme returns "host:port" from "http://host:port".
func stripScheme(s string) string {
	s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	return s
}

// slugify produces a route-id-safe slug from a path or regex pattern.
func slugify(in string) string {
	in = strings.Trim(in, "/^$")
	in = strings.ToLower(in)
	var b strings.Builder
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '/' || r == '-' || r == '_':
			if b.Len() > 0 && b.String()[b.Len()-1] != '-' {
				b.WriteRune('-')
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// ---------------------------------------------------------------------------
// Patterns
// ---------------------------------------------------------------------------

// extractNginxSetVars finds every `set $name "value";` or `set $name value;`
// directive in body and returns a name→value map. Only used for prod-fallback
// expansion in @fallback locations.
func extractNginxSetVars(body string) map[string]string {
	out := map[string]string{}
	for _, m := range nginxSetRe.FindAllStringSubmatch(body, -1) {
		if len(m) >= 3 {
			v := strings.TrimSpace(m[2])
			v = strings.Trim(v, `"`)
			out[m[1]] = v
		}
	}
	return out
}

// expandNginxVars substitutes $name references in s using vars; unknown names
// are left untouched.
func expandNginxVars(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "$"+k, v)
	}
	return s
}

var (
	listen443               = regexp.MustCompile(`(?m)^\s*listen\s+443\b`)
	listen80                = regexp.MustCompile(`(?m)^\s*listen\s+80\b`)
	matchListen             = regexp.MustCompile(`(?m)^\s*listen\s+\d+\b`)
	serverNameRe            = regexp.MustCompile(`(?m)^\s*server_name\s+([^;]+);`)
	clientMaxBodyRe         = regexp.MustCompile(`(?m)^\s*client_max_body_size\s+([^;]+);`)
	fastcgiReadTimeoutRe    = regexp.MustCompile(`(?m)fastcgi_read_timeout\s+([^;]+);`)
	fastcgiSendTimeoutRe    = regexp.MustCompile(`(?m)fastcgi_send_timeout\s+([^;]+);`)
	fastcgiConnectTimeoutRe = regexp.MustCompile(`(?m)fastcgi_connect_timeout\s+([^;]+);`)
	namedLocationRe         = regexp.MustCompile(`location\s+@([A-Za-z0-9_]+)\s*\{([^{}]*)\}`)
	errorPageRe             = regexp.MustCompile(`error_page\s+5\d\d(?:\s+5\d\d)*\s*=\s*@([A-Za-z0-9_]+)`)
	plainLocationRe         = regexp.MustCompile(`(?ms)location\s+(/[A-Za-z0-9_./-]*)\s*\{([^{}]*)\}`)
	regexLocationRe         = regexp.MustCompile(`(?ms)location\s+~\*?\s+([^{]+?)\s*\{([^{}]*)\}`)
	proxyPassRe             = regexp.MustCompile(`proxy_pass\s+([^;\s]+)\s*;`)
	rewriteRe               = regexp.MustCompile(`rewrite\s+([^\s]+)\s+([^\s]+)\s+(?:last|break|redirect|permanent)\s*;`)
	nginxSetRe              = regexp.MustCompile(`(?m)^\s*set\s+\$([A-Za-z_][A-Za-z0-9_]*)\s+("[^"]*"|[^;\s]+)\s*;`)
)
