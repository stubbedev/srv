// Package cmd — validate.go implements `srv validate` which parses + checks
// a site's metadata.yml without applying anything.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

var validateFlags struct {
	all bool
}

var validateCmd = &cobra.Command{
	Use:   "validate [SITE]",
	Short: "Validate a site's metadata.yml without applying changes",
	RunE:  runValidate,
	Args: func(cmd *cobra.Command, args []string) error {
		if validateFlags.all {
			return cobra.NoArgs(cmd, args)
		}
		return cobra.ExactArgs(1)(cmd, args)
	},
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	validateCmd.Flags().BoolVarP(&validateFlags.all, "all", "a", false, "Validate all registered sites")
	validateCmd.GroupID = GroupSites
	RootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) error {
	var names []string
	if validateFlags.all {
		names = GetSiteNames()
	} else {
		names = []string{args[0]}
	}

	failed := 0
	for _, name := range names {
		if err := validateOne(name); err != nil {
			ui.Warn("%s: %v", name, err)
			failed++
			continue
		}
		ui.Success("%s: ok", name)
	}
	if failed > 0 {
		return fmt.Errorf("%d site(s) failed validation", failed)
	}
	return nil
}

func validateOne(name string) error {
	meta, err := site.ReadSiteMetadata(name)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("site not found")
	}
	return site.ValidateMetadata(meta)
}
