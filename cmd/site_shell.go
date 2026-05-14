// Package cmd — site_shell.go implements the interactive site commands:
// `srv shell` (open a shell in the site's container) and `srv open`
// (open the site's URL in the system browser).
package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/pool"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// shell command
// =============================================================================

var shellFlags struct {
	service string
}

var shellCmd = &cobra.Command{
	Use:   "shell SITE",
	Short: "Open an interactive shell in a site's container",
	Long: `Open an interactive shell (sh) in the primary container for a site.

For PHP sites the default is the php-fpm container (srv-SITE-php).
Use --service web to get a shell in the nginx container instead.

For Node, Ruby, Python, and Dockerfile sites the single app container is used.

For compose sites the first service container is used; use --service to pick one.

Examples:
  srv site shell mysite
  srv site shell mysite --service web   # nginx container for PHP sites`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv site shell SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv site shell SITE", "too many arguments — expected a single site name, got %d", len(args))
		}
		return nil
	},
	RunE: runShell,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	shellCmd.Flags().StringVar(&shellFlags.service, "service", "", "Container name or service to shell into")
	shellCmd.GroupID = GroupSites
	RootCmd.AddCommand(shellCmd)
}

func runShell(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	siteName := args[0]
	s, err := site.GetByName(siteName)
	if err != nil {
		return err
	}

	if s.IsBroken {
		return fmt.Errorf("site '%s' is broken (target directory missing)", s.Name)
	}

	// Determine the container to shell into.
	containerName := shellFlags.service
	if containerName == "" {
		containerName = siteShellContainer(*s)
	}

	if containerName == "" {
		return fmt.Errorf("cannot determine container for site '%s' — use --service to specify one", siteName)
	}

	if !docker.ContainerExists(containerName) {
		return fmt.Errorf("container '%s' is not running — start the site first with: srv start %s", containerName, siteName)
	}

	ui.Dim("Connecting to container: %s", containerName)
	execArgs := []string{"exec", "-it"}
	// For PHP sites the shell lands in the shared pool container; set the
	// working directory to this site's mount so paths feel per-site.
	if s.Type == site.SiteTypePHP {
		execArgs = append(execArgs, "-w", "/var/www/"+siteName)
	}
	execArgs = append(execArgs, containerName, "sh")
	c := exec.Command("docker", execArgs...) //nolint:gosec
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		// Exit code != 0 from the shell is normal (user typed exit N), don't wrap it as an error.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() != 0 {
			return nil
		}
		return fmt.Errorf("docker exec failed: %w", err)
	}
	return nil
}

// siteShellContainer returns the container name to shell into for a given site.
// For PHP sites this is the shared pool container; the caller is expected to
// set the working directory to /var/www/<sitename> when execing.
func siteShellContainer(s site.Site) string {
	switch s.Type {
	case site.SiteTypePHP:
		return phpFPMContainerForSite(s.Name)
	case site.SiteTypeNode:
		return "srv-" + s.Name + "-node"
	case site.SiteTypeRuby, site.SiteTypePython, site.SiteTypeDockerfile:
		return "srv-" + s.Name + "-app"
	default:
		// Compose sites: use the stored service name (container name).
		return s.ServiceName
	}
}

// phpFPMContainerForSite resolves a PHP site to its shared FPM container name
// by reading the site's metadata and computing the pool fingerprint. Falls
// back to the legacy per-site container name if metadata is missing.
func phpFPMContainerForSite(siteName string) string {
	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil || meta == nil {
		return "srv-" + siteName + "-php"
	}
	exts := make([]string, 0, len(meta.PHPExtensions))
	for _, e := range meta.PHPExtensions {
		if !site.IsBuiltinPHPExtension(e) {
			exts = append(exts, e)
		}
	}
	return "srv-fpm-" + pool.Fingerprint(meta.PHPVersion, exts)
}

// =============================================================================
// open command
// =============================================================================

var openCmd = &cobra.Command{
	Use:   "open SITE",
	Short: "Open a site in the default browser",
	Long:  `Open the site's HTTPS URL in the system default browser using xdg-open.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv site open SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv site open SITE", "too many arguments — expected a single site name, got %d", len(args))
		}
		return nil
	},
	RunE: runOpen,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	openCmd.GroupID = GroupSites
	RootCmd.AddCommand(openCmd)
}

func runOpen(cmd *cobra.Command, args []string) error {
	siteName := args[0]
	s, err := site.GetByName(siteName)
	if err != nil {
		return err
	}

	primary := s.Domain()
	if primary == "" {
		return fmt.Errorf("site '%s' has no domain configured", siteName)
	}

	url := "https://" + primary
	ui.Dim("Opening %s...", url)
	c := exec.Command("xdg-open", url) //nolint:gosec
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("xdg-open failed: %w", err)
	}
	return nil
}
