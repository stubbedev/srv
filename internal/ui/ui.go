// Package ui provides terminal output helpers for the srv CLI.
//
// Design goals (in priority order):
//
//  1. Scriptable. Results go to stdout, diagnostics (Info/Warn/Dim/Success
//     status messages) go to stderr. Pipe-safe by default: NO_COLOR is
//     honoured and ANSI colour codes are auto-disabled when stdout/stderr
//     isn't a TTY. No interactive prompts in this package — callers must
//     either drive the run via flags or read stdin themselves.
//  2. Plain. No Unicode icons; the colour alone signals severity. The
//     message text always carries the semantics so output remains
//     greppable even when colour is stripped.
//  3. Minimal deps. Uses fatih/color for the styling layer; no charm /
//     lipgloss / huh / bubbletea.
package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/fatih/color"

	"github.com/stubbedev/srv/internal/constants"
)

var (
	// Verbose unlocks VerboseLog. Off by default.
	Verbose bool
	// Quiet suppresses Info/Warn/Dim/Success diagnostic lines. Error and
	// Print (result) output still go through.
	Quiet bool

	// printMu serialises stdout/stderr writes for the parallel-operation helpers.
	printMu sync.Mutex

	// Colour functions. fatih/color disables itself automatically when the
	// destination isn't a TTY or NO_COLOR is set, so callers can use these
	// unconditionally — output stays clean in pipes and CI logs.
	successC = color.New(color.FgGreen).SprintFunc()
	errorC   = color.New(color.FgRed).SprintFunc()
	warnC    = color.New(color.FgYellow).SprintFunc()
	infoC    = color.New(color.FgBlue).SprintFunc()
	dimC     = color.New(color.FgHiBlack).SprintFunc()
	boldC    = color.New(color.Bold).SprintFunc()
	cyanC    = color.New(color.FgCyan).SprintFunc()
	purpleC  = color.New(color.FgMagenta).SprintFunc()
)

// outStdout / outStderr are the destinations for diagnostic / result output.
// Exposed as vars so tests can swap them.
var (
	outStdout io.Writer = os.Stdout
	outStderr io.Writer = os.Stderr
)

// Steps tracks progress through a multi-step operation. Output goes to stderr
// (it's diagnostic, not result data).
type Steps struct {
	total   int
	current int
}

// NewSteps creates a new step tracker.
func NewSteps(total int) *Steps { return &Steps{total: total} }

// Next advances to the next step and prints the message.
func (s *Steps) Next(format string, args ...any) {
	s.current++
	if Quiet {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(outStderr, "%s %s\n", dimC(fmt.Sprintf("[%d/%d]", s.current, s.total)), msg)
}

// Done prints a completion message for the current step.
func (s *Steps) Done(format string, args ...any) {
	if Quiet {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(outStderr, "%s %s\n", dimC(fmt.Sprintf("[%d/%d]", s.current, s.total)), successC(msg))
}

// Skip prints a skip message for the current step.
func (s *Steps) Skip(format string, args ...any) {
	if Quiet {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(outStderr, "%s %s\n", dimC(fmt.Sprintf("[%d/%d]", s.current, s.total)), dimC(msg))
}

// Success writes a diagnostic success line to stderr. Suppressed under --quiet.
func Success(format string, args ...any) {
	if Quiet {
		return
	}
	fmt.Fprintln(outStderr, successC(fmt.Sprintf(format, args...)))
}

// Error writes an error line to stderr. Never suppressed.
func Error(format string, args ...any) {
	fmt.Fprintln(outStderr, errorC(fmt.Sprintf(format, args...)))
}

// Warn writes a warning line to stderr. Suppressed under --quiet.
func Warn(format string, args ...any) {
	if Quiet {
		return
	}
	fmt.Fprintln(outStderr, warnC(fmt.Sprintf(format, args...)))
}

// Info writes an informational diagnostic line to stderr. Suppressed under
// --quiet. Use Print for actual command results that should be pipeable.
func Info(format string, args ...any) {
	if Quiet {
		return
	}
	fmt.Fprintln(outStderr, infoC(fmt.Sprintf(format, args...)))
}

// Dim writes a low-emphasis diagnostic line to stderr. Suppressed under --quiet.
func Dim(format string, args ...any) {
	if Quiet {
		return
	}
	fmt.Fprintln(outStderr, dimC(fmt.Sprintf(format, args...)))
}

// Bold writes a bold diagnostic line to stderr. Suppressed under --quiet.
func Bold(format string, args ...any) {
	if Quiet {
		return
	}
	fmt.Fprintln(outStderr, boldC(fmt.Sprintf(format, args...)))
}

// Code writes a code-styled diagnostic line to stderr (kept for backwards
// compat with existing callsites that want a cyan-ish tone).
func Code(format string, args ...any) {
	if Quiet {
		return
	}
	fmt.Fprintln(outStderr, cyanC(fmt.Sprintf(format, args...)))
}

// Print writes a result line to STDOUT — what a script would consume. No
// colour, no suppression. Use for the actual data the command produced.
func Print(format string, args ...any) {
	fmt.Fprintln(outStdout, fmt.Sprintf(format, args...))
}

// Blank emits a blank line on stderr (alignment for diagnostic output).
func Blank() {
	if Quiet {
		return
	}
	fmt.Fprintln(outStderr)
}

// UsageError returns a formatted error including a usage hint. Used by
// command Pre/Run handlers when a required flag is missing.
func UsageError(usage, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	hint := dimC("  usage: " + usage)
	return fmt.Errorf("%s\n%s", msg, hint)
}

// VerboseLog writes to stderr only when Verbose is true.
func VerboseLog(format string, args ...any) {
	if !Verbose {
		return
	}
	fmt.Fprintln(outStderr, dimC(fmt.Sprintf(format, args...)))
}

// Indent returns a string with the given indentation level.
func Indent(level int, format string, args ...any) string {
	return strings.Repeat(constants.IndentString, level) + fmt.Sprintf(format, args...)
}

// IndentedSuccess writes an indented success line to stderr.
func IndentedSuccess(level int, format string, args ...any) {
	if Quiet {
		return
	}
	fmt.Fprintln(outStderr, successC(Indent(level, format, args...)))
}

// IndentedError writes an indented error line to stderr. Never suppressed.
func IndentedError(level int, format string, args ...any) {
	fmt.Fprintln(outStderr, errorC(Indent(level, format, args...)))
}

// IndentedWarn writes an indented warning to stderr.
func IndentedWarn(level int, format string, args ...any) {
	if Quiet {
		return
	}
	fmt.Fprintln(outStderr, warnC(Indent(level, format, args...)))
}

// IndentedInfo writes an indented info line to stderr.
func IndentedInfo(level int, format string, args ...any) {
	if Quiet {
		return
	}
	fmt.Fprintln(outStderr, infoC(Indent(level, format, args...)))
}

// IndentedDim writes an indented dim line to stderr.
func IndentedDim(level int, format string, args ...any) {
	if Quiet {
		return
	}
	fmt.Fprintln(outStderr, dimC(Indent(level, format, args...)))
}

// =============================================================================
// Thread-safe variants for parallel operations
// =============================================================================

// SafeIndentedDim writes a dim line under a mutex.
func SafeIndentedDim(level int, format string, args ...any) {
	if Quiet {
		return
	}
	printMu.Lock()
	defer printMu.Unlock()
	fmt.Fprintln(outStderr, dimC(Indent(level, format, args...)))
}

// SafeError writes an error line under a mutex.
func SafeError(format string, args ...any) {
	printMu.Lock()
	defer printMu.Unlock()
	fmt.Fprintln(outStderr, errorC(fmt.Sprintf(format, args...)))
}

// SafeWarn writes a warning line under a mutex.
func SafeWarn(format string, args ...any) {
	if Quiet {
		return
	}
	printMu.Lock()
	defer printMu.Unlock()
	fmt.Fprintln(outStderr, warnC(fmt.Sprintf(format, args...)))
}

// =============================================================================
// Text colour helpers — colour a string in-place for tables / inline use
// =============================================================================

// StatusColor returns the status string with a colour appropriate to the value.
// Returns the bare string for unknown values.
func StatusColor(status string) string {
	switch status {
	case "running", "valid", "active":
		return successC(status)
	case "stopped", "auto", "inactive":
		return dimC(status)
	case "broken", "expired", "missing", "failed":
		return errorC(status)
	case "expiring":
		return warnC(status)
	default:
		if strings.HasPrefix(status, constants.StatusPartial) {
			return warnC(status)
		}
		return status
	}
}

// TypeColor returns "local" or "production" tinted for inline use in tables.
func TypeColor(isLocal bool) string {
	if isLocal {
		return cyanC("local")
	}
	return successC("production")
}

// Highlight returns text with the highlight colour.
func Highlight(s string) string { return cyanC(s) }

// SuccessText / ErrorText / WarnText / InfoText / DimText return their input
// tinted with the corresponding colour.
func SuccessText(s string) string { return successC(s) }
func ErrorText(s string) string   { return errorC(s) }
func WarnText(s string) string    { return warnC(s) }
func InfoText(s string) string    { return infoC(s) }
func DimText(s string) string     { return dimC(s) }
func AccentText(s string) string  { return purpleC(s) }

// =============================================================================
// Table output — plain ASCII, no lipgloss
// =============================================================================

// PrintTable writes a column-aligned table to STDOUT (results, not diagnostics).
// Headers are bold; rows are written verbatim so callers can pre-colour cells
// with StatusColor / TypeColor / DimText etc.
//
// Width is computed from the visible character count (ANSI sequences stripped)
// so coloured cells don't throw alignment off.
func PrintTable(headers []string, rows [][]string) {
	if len(headers) == 0 && len(rows) == 0 {
		return
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(stripAnsi(h))
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			if w := len(stripAnsi(cell)); w > widths[i] {
				widths[i] = w
			}
		}
	}

	// Header
	for i, h := range headers {
		visible := stripAnsi(h)
		padding := widths[i] - len(visible)
		fmt.Fprint(outStdout, boldC(h)+strings.Repeat(" ", padding))
		if i < len(headers)-1 {
			fmt.Fprint(outStdout, "  ")
		}
	}
	fmt.Fprintln(outStdout)

	// Rows
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			visible := stripAnsi(cell)
			padding := widths[i] - len(visible)
			fmt.Fprint(outStdout, cell+strings.Repeat(" ", padding))
			if i < len(row)-1 {
				fmt.Fprint(outStdout, "  ")
			}
		}
		fmt.Fprintln(outStdout)
	}
}

// PrintJSON writes the given value to STDOUT as indented JSON followed by a
// newline. Used by `--format json` on list commands so scripts can parse the
// output without scraping a colour-tinted table.
func PrintJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if _, err := outStdout.Write(data); err != nil {
		return err
	}
	_, err = outStdout.Write([]byte{'\n'})
	return err
}

// stripAnsi removes ANSI/VT100 escape sequences so PrintTable can compute
// visible column widths.
func stripAnsi(s string) string {
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}
