// Package cmd — volume.go implements `srv volume` for managing extra
// bind-mounts on a site's container. Used to expose host paths (TEMP dirs,
// nix-profile binaries, demo asset trees) into the site's container.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

var volumeCmd = &cobra.Command{
	Use:   "volume",
	Short: "Manage extra host bind-mounts attached to a site",
	Long: `Add or remove host directories that should be bind-mounted into the
site's container.

Useful for exposing tooling installed on the host (e.g. ~/.nix-profile),
shared temp directories, or static asset trees that live outside the
project root.

Each mount is specified as HOST:CONTAINER[:ro] where both paths are
absolute. A trailing :ro makes the mount read-only.

Examples:
  srv volume add app ~/.nix-profile:/home/$USER/.nix-profile:ro
  srv volume add app /nix:/nix:ro
  srv volume add app /tmp/uploads:/tmp/uploads`,
}

var volumeAddCmd = &cobra.Command{
	Use:   "add SITE HOST:CONTAINER[:ro]",
	Short: "Attach a bind-mount to a site",
	Args:  cobra.ExactArgs(2),
	RunE:  runVolumeAdd,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

var volumeRemoveCmd = &cobra.Command{
	Use:     "remove SITE TARGET",
	Aliases: []string{"rm", "detach"},
	Short:   "Remove a bind-mount from a site by its container target path",
	Args:    cobra.ExactArgs(2),
	RunE:    runVolumeRemove,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
		}
		if len(args) == 1 {
			return GetSiteVolumeTargets(args[0]), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

var volumeListCmd = &cobra.Command{
	Use:   "list SITE",
	Short: "List bind-mounts attached to a site",
	Args:  cobra.ExactArgs(1),
	RunE:  runVolumeList,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	volumeCmd.GroupID = GroupSites
	volumeCmd.AddCommand(volumeAddCmd, volumeRemoveCmd, volumeListCmd)
	RootCmd.AddCommand(volumeCmd)
}

func runVolumeAdd(cmd *cobra.Command, args []string) error {
	siteName, spec := args[0], args[1]
	mount, err := ParseVolumeSpec(spec)
	if err != nil {
		return err
	}

	// Orchestration shared with the MCP add_volume tool.
	warnings, err := site.AddVolume(siteName, mount)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		ui.Warn("%s", w)
	}
	ui.Success("Attached %s → %s%s to %s", mount.Source, mount.Target, roSuffix(mount.ReadOnly), siteName)
	ui.Dim("Run 'srv restart %s' for the change to take effect.", siteName)
	return nil
}

func runVolumeRemove(cmd *cobra.Command, args []string) error {
	siteName, target := args[0], args[1]

	warnings, err := site.RemoveVolume(siteName, target)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		ui.Warn("%s", w)
	}
	ui.Success("Removed volume %s from %s", target, siteName)
	ui.Dim("Run 'srv restart %s' for the change to take effect.", siteName)
	return nil
}

// volumeListOut is the json shape for `srv volume list --format json`.
type volumeListOut struct {
	Site    string             `json:"site"`
	Volumes []site.VolumeMount `json:"volumes"`
}

func runVolumeList(cmd *cobra.Command, args []string) error {
	siteName := args[0]
	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("site not found: %s", siteName)
	}
	if jsonOutput() {
		return ui.PrintJSON(volumeListOut{Site: siteName, Volumes: meta.Volumes})
	}
	if len(meta.Volumes) == 0 {
		ui.Dim("No extra volumes attached to %s", siteName)
		return nil
	}
	for _, v := range meta.Volumes {
		ui.Print("  %s → %s%s", v.Source, v.Target, roSuffix(v.ReadOnly))
	}
	return nil
}

func roSuffix(ro bool) string {
	if ro {
		return " (ro)"
	}
	return ""
}

// ParseVolumeSpec parses a HOST:CONTAINER[:ro] spec into a site.VolumeMount.
// Source path expansion: a leading `~` is expanded to $HOME. Both source and
// target must be absolute after expansion. The source must exist on disk.
func ParseVolumeSpec(spec string) (site.VolumeMount, error) {
	parts := strings.Split(spec, ":")
	var source, target string
	readOnly := false

	switch len(parts) {
	case 2:
		source, target = parts[0], parts[1]
	case 3:
		source, target = parts[0], parts[1]
		switch parts[2] {
		case "ro", "readonly":
			readOnly = true
		case "rw":
			readOnly = false
		default:
			return site.VolumeMount{}, fmt.Errorf("invalid volume option %q — expected 'ro' or 'rw'", parts[2])
		}
	default:
		return site.VolumeMount{}, fmt.Errorf("volume must be HOST:CONTAINER[:ro], got %q", spec)
	}

	source = expandHome(strings.TrimSpace(source))
	target = strings.TrimSpace(target)

	if source == "" || target == "" {
		return site.VolumeMount{}, fmt.Errorf("source and target are required")
	}
	if !filepath.IsAbs(source) {
		return site.VolumeMount{}, fmt.Errorf("source %q must be an absolute path (use ~/… or /…)", source)
	}
	if !filepath.IsAbs(target) {
		return site.VolumeMount{}, fmt.Errorf("target %q must be an absolute container path", target)
	}
	if _, err := os.Stat(source); err != nil {
		return site.VolumeMount{}, fmt.Errorf("source path %q does not exist on host", source)
	}

	return site.VolumeMount{Source: source, Target: target, ReadOnly: readOnly}, nil
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// GetSiteVolumeTargets returns the volume targets attached to a site, for shell completion.
func GetSiteVolumeTargets(siteName string) []string {
	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil || meta == nil {
		return nil
	}
	out := make([]string, 0, len(meta.Volumes))
	for _, v := range meta.Volumes {
		out = append(out, v.Target)
	}
	return out
}
