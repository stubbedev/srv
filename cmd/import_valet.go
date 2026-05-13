// Package cmd — import_valet.go implements `srv import valet` which converts
// ~/.valet/Nginx/* configurations into srv commands.
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/importers/valet"
	"github.com/stubbedev/srv/internal/ui"
)

var importFlags struct {
	valetDir string
	apply    bool
}

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import site configurations from other tools",
}

var importValetCmd = &cobra.Command{
	Use:   "valet",
	Short: "Translate ~/.valet/Nginx/* into srv commands",
	Long: `Reads every Valet nginx config in --valet-dir (default ~/.valet) and prints
the equivalent srv commands. Recognises PHP/FastCGI sites, reverse proxies,
:88 internal listeners, /path → port splits, regex rewrite locations, and
@fallback prod-mirror locations.

Default mode is dry-run: it only prints. Pass --apply to execute each
command via the same shell.`,
	RunE: runImportValet,
}

func init() {
	importValetCmd.Flags().StringVar(&importFlags.valetDir, "valet-dir", "", "Path to valet config dir (default ~/.valet)")
	importValetCmd.Flags().BoolVar(&importFlags.apply, "apply", false, "Execute the generated srv commands instead of just printing them")
	importCmd.AddCommand(importValetCmd)
	importCmd.GroupID = GroupSystem
	RootCmd.AddCommand(importCmd)
}

func runImportValet(cmd *cobra.Command, args []string) error {
	valetDir, err := resolveValetDir(importFlags.valetDir)
	if err != nil {
		return err
	}
	ui.Dim("Using valet dir: %s", valetDir)
	nginxDir := filepath.Join(valetDir, "Nginx")
	sitesDir := filepath.Join(valetDir, "Sites")

	if _, err := os.Stat(nginxDir); err != nil {
		return fmt.Errorf("cannot read %s: %w", nginxDir, err)
	}

	// Pull parked paths from config.json so directory-name mode works.
	cfg := valet.ReadConfig(valetDir)

	sites, err := valet.ParseDir(nginxDir, sitesDir, cfg.Paths)
	if err != nil {
		return err
	}
	if len(sites) == 0 {
		ui.Dim("No Valet configurations found in %s", nginxDir)
		return nil
	}

	plan := buildImportPlan(sites)
	if len(plan) == 0 {
		ui.Dim("Nothing to import.")
		return nil
	}

	for i, step := range plan {
		ui.Print("  [%d] %s", i+1, step.line)
		for _, note := range step.notes {
			ui.IndentedDim(2, "%s", note)
		}
	}

	if !importFlags.apply {
		ui.Blank()
		ui.Dim("Dry-run: rerun with --apply to execute these commands.")
		return nil
	}

	ui.Blank()
	ui.Info("Applying %d command(s)...", len(plan))
	srvBinary, err := os.Executable()
	if err != nil {
		srvBinary = "srv"
	}
	executed := 0
	skipped := 0
	for i, step := range plan {
		if len(step.args) == 0 {
			// Commented / unresolved entry — skip during --apply.
			skipped++
			continue
		}
		cmd := exec.Command(srvBinary, step.args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("step %d (%s) failed: %w", i+1, step.line, err)
		}
		executed++
	}
	if skipped > 0 {
		ui.Warn("Skipped %d entry/ies with unresolved fields (run dry-run to see them)", skipped)
	}
	ui.Success("Imported %d entry/ies", executed)
	return nil
}

// resolveValetDir picks a valet config directory. Order: explicit --valet-dir,
// then ~/.config/valet (XDG), then ~/.valet (legacy). The chosen directory
// must exist and contain an `Nginx/` subdirectory.
func resolveValetDir(explicit string) (string, error) {
	if explicit != "" {
		return explicit, validateValetDir(explicit)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	candidates := []string{
		filepath.Join(home, ".config", "valet"),
		filepath.Join(home, ".valet"),
	}
	var firstErr error
	for _, c := range candidates {
		if err := validateValetDir(c); err == nil {
			return c, nil
		} else if firstErr == nil {
			firstErr = err
		}
	}
	return "", fmt.Errorf("could not find a valet config dir; tried %v: %w", candidates, firstErr)
}

func validateValetDir(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "Nginx")); err != nil {
		return fmt.Errorf("%s has no Nginx/ subdir: %w", dir, err)
	}
	return nil
}

// importStep is one srv invocation produced by the planner. `line` is the
// human-readable form printed in dry-run; `args` is the actual argv used by
// --apply.
type importStep struct {
	line  string
	args  []string
	notes []string
}

// importGroup folds multiple valet nginx files that resolve to the same
// project directory into one srv site (canonical + aliases).
type importGroup struct {
	canonical *valet.Site
	aliases   []*valet.Site
}

func buildImportPlan(sites []*valet.Site) []importStep {
	var plan []importStep
	// Group by project path so multi-host kontainer-style sites collapse into
	// one srv add with --alias entries. Proxies are not grouped (their target
	// upstream is what makes them unique).
	groups := map[string]*importGroup{}
	var order []string
	var loose []*valet.Site
	for _, s := range sites {
		if !s.IsPHP || s.ProjectPath == "" {
			loose = append(loose, s)
			continue
		}
		g, ok := groups[s.ProjectPath]
		if !ok {
			g = &importGroup{canonical: s}
			groups[s.ProjectPath] = g
			order = append(order, s.ProjectPath)
			continue
		}
		g.aliases = append(g.aliases, s)
	}

	for _, key := range order {
		g := groups[key]
		plan = append(plan, planPHPSite(g))
	}
	for _, s := range loose {
		if step, ok := planLooseSite(s); ok {
			plan = append(plan, step)
		}
	}
	return plan
}

func planPHPSite(g *importGroup) importStep {
	s := g.canonical
	args := []string{"add", s.ProjectPath, "--domain", s.Domain, "--local"}
	if s.Wildcard {
		args = append(args, "--wildcard")
	}
	if s.Internal {
		args = append(args, "--internal-http")
	}
	for _, alias := range g.aliases {
		// Aliases inherit wildcard/internal flags from the canonical site;
		// no per-alias overrides supported in `srv add`.
		args = append(args, "--alias", alias.Domain)
		for _, extra := range alias.Aliases {
			args = append(args, "--alias", extra)
		}
	}
	for _, a := range s.Aliases {
		args = append(args, "--alias", a)
	}
	addLimitFlags(&args, s)

	notes := []string{}
	for _, r := range s.Routes {
		notes = append(notes, fmt.Sprintf("post-add: srv route add <name> %s", routeFlags(r)))
	}
	for _, alias := range g.aliases {
		for _, r := range alias.Routes {
			notes = append(notes, fmt.Sprintf("post-add (from %s): srv route add <name> %s", alias.Domain, routeFlags(r)))
		}
	}
	for _, n := range s.UnknownNotes {
		notes = append(notes, "unhandled: "+n)
	}
	return importStep{line: "srv " + strings.Join(args, " "), args: args, notes: notes}
}

// planLooseSite emits a step for non-PHP entries (proxies, unresolved PHP).
// Returns ok=false when there's nothing actionable.
func planLooseSite(s *valet.Site) (importStep, bool) {
	if s.Domain == "" {
		return importStep{}, false
	}
	if s.ProxyTarget != "" {
		port := portFromHostPort(s.ProxyTarget)
		if port == 0 {
			return importStep{}, false
		}
		args := []string{"proxy", "add", "-d", s.Domain, "-p", fmt.Sprintf("%d", port)}
		if s.Wildcard {
			args = append(args, "--wildcard")
		}
		if s.FallbackURL != "" {
			args = append(args, "--fallback", s.FallbackURL)
		}
		notes := []string{}
		for _, r := range s.Routes {
			notes = append(notes, fmt.Sprintf("post-add: srv route add %s %s", s.Domain, routeFlags(r)))
		}
		for _, n := range s.UnknownNotes {
			notes = append(notes, "unhandled: "+n)
		}
		return importStep{line: "srv " + strings.Join(args, " "), args: args, notes: notes}, true
	}
	// PHP site whose project path we couldn't resolve. Emit a fully-formed
	// `srv add` line with <PROJECT_PATH> placeholder so the user only has
	// to fill the path. Still printed as a comment so --apply skips it.
	if s.IsPHP {
		args := []string{"add", "<PROJECT_PATH>", "--domain", s.Domain, "--local"}
		if s.Wildcard {
			args = append(args, "--wildcard")
		}
		if s.Internal {
			args = append(args, "--internal-http")
		}
		for _, a := range s.Aliases {
			args = append(args, "--alias", a)
		}
		addLimitFlags(&args, s)
		notes := []string{}
		for _, r := range s.Routes {
			notes = append(notes, fmt.Sprintf("post-add: srv route add <name> %s", routeFlags(r)))
		}
		notes = append(notes, fmt.Sprintf("source: %s", filepath.Base(s.File)))
		return importStep{
			line:  "# unresolved (fill in <PROJECT_PATH>): srv " + strings.Join(args, " "),
			args:  nil, // never executed via --apply because line is commented
			notes: notes,
		}, true
	}
	return importStep{}, false
}

func addLimitFlags(args *[]string, s *valet.Site) {
	if s.MaxBody != "" {
		*args = append(*args, "--max-body", s.MaxBody)
	}
	if s.ReadTimeout != "" {
		*args = append(*args, "--read-timeout", s.ReadTimeout)
	}
	if s.SendTimeout != "" {
		*args = append(*args, "--send-timeout", s.SendTimeout)
	}
	if s.ConnTimeout != "" {
		*args = append(*args, "--connect-timeout", s.ConnTimeout)
	}
}

func routeFlags(r valet.Route) string {
	parts := []string{}
	if r.Path != "" {
		parts = append(parts, "--path", r.Path)
	}
	if r.PathRegex != "" {
		parts = append(parts, "--path-regex", fmt.Sprintf("'%s'", r.PathRegex))
	}
	if r.Rewrite != "" {
		parts = append(parts, "--rewrite", fmt.Sprintf("'%s'", r.Rewrite))
	}
	if r.Port != 0 {
		parts = append(parts, "--port", fmt.Sprintf("%d", r.Port))
	}
	return strings.Join(parts, " ")
}

func portFromHostPort(hp string) int {
	if i := strings.LastIndex(hp, ":"); i >= 0 {
		var p int
		_, _ = fmt.Sscanf(hp[i+1:], "%d", &p)
		return p
	}
	return 0
}
