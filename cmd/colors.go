package cmd

import (
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// setupColoredHelp configures custom colored help output for Cobra
func setupColoredHelp() {
	// Colors (auto-detects TTY for color support)
	yellow := color.New(color.FgYellow, color.Bold).SprintFunc()
	green := color.New(color.FgGreen, color.Bold).SprintFunc()
	red := color.New(color.FgRed, color.Bold).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()

	// Add template functions for coloring
	cobra.AddTemplateFunc("styleHeading", yellow)
	cobra.AddTemplateFunc("styleCommand", green)
	cobra.AddTemplateFunc("styleFlag", red)
	cobra.AddTemplateFunc("styleCyan", cyan)
	cobra.AddTemplateFunc("colorizeFlags", func(s string) string {
		// Color flag names (lines starting with spaces followed by -)
		var lines strings.Builder
		for _, line := range splitLines(s) {
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				// This is a flag line, color the flag name
				colored := colorFlagLine(line, red)
				lines.WriteString(colored + "\n")
			} else {
				lines.WriteString(line + "\n")
			}
		}
		return lines.String()
	})

	// Set the custom template
	template := `{{styleHeading "Usage:"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{styleHeading "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{styleHeading "Examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{styleHeading "Available Commands:"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{styleCommand (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{styleHeading .Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{styleCommand (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{styleHeading "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{styleCommand (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{styleHeading "Flags:"}}

{{colorizeFlags (.LocalFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}{{if .HasAvailableInheritedFlags}}

{{styleHeading "Global Flags:"}}

{{colorizeFlags (.InheritedFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}{{if .HasHelpSubCommands}}

{{styleHeading "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use {{styleCyan "\"srv [command] --help\""}} for more information about a command.{{end}}
`
	RootCmd.SetUsageTemplate(template)
}

// splitLines splits a string into lines
func splitLines(s string) []string {
	var lines []string
	var current string
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(r)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// colorFlagLine colors the flag portion of a flag usage line
func colorFlagLine(line string, colorFunc func(...any) string) string {
	// Find where the flag starts and where the description starts
	// Format is typically: "  -f, --flag string   Description here"
	trimmed := line
	leadingSpace := ""

	// Extract leading whitespace
	for i, r := range line {
		if r != ' ' && r != '\t' {
			leadingSpace = line[:i]
			trimmed = line[i:]
			break
		}
	}

	// Find where description starts (after multiple spaces)
	descStart := -1
	spaceCount := 0
	for i, r := range trimmed {
		if r == ' ' {
			spaceCount++
			if spaceCount >= 2 && descStart == -1 {
				descStart = i
			}
		} else {
			if spaceCount >= 2 {
				break
			}
			spaceCount = 0
		}
	}

	if descStart > 0 {
		flagPart := trimmed[:descStart]
		descPart := trimmed[descStart:]
		return leadingSpace + colorFunc(flagPart) + descPart
	}

	// If we can't find the split, just color the whole thing
	return leadingSpace + colorFunc(trimmed)
}
