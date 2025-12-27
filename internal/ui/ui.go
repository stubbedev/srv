// Package ui provides terminal UI styling and helpers.
package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Color palette
	green  = lipgloss.Color("10")
	red    = lipgloss.Color("9")
	yellow = lipgloss.Color("11")
	blue   = lipgloss.Color("12")
	gray   = lipgloss.Color("8")
	cyan   = lipgloss.Color("14")
	white  = lipgloss.Color("15")

	// Styles
	SuccessStyle = lipgloss.NewStyle().Foreground(green)
	ErrorStyle   = lipgloss.NewStyle().Foreground(red)
	WarnStyle    = lipgloss.NewStyle().Foreground(yellow)
	InfoStyle    = lipgloss.NewStyle().Foreground(blue)
	DimStyle     = lipgloss.NewStyle().Foreground(gray)
	CyanStyle    = lipgloss.NewStyle().Foreground(cyan)
	BoldStyle    = lipgloss.NewStyle().Bold(true)
	CodeStyle    = lipgloss.NewStyle().Foreground(cyan)

	// Verbose mode flag
	Verbose bool

	// Mutex for thread-safe output
	printMu sync.Mutex
)

// Status symbols (using ASCII for compatibility)
const (
	SymbolSuccess = "[ok]"
	SymbolError   = "[error]"
	SymbolWarning = "[warn]"
	SymbolInfo    = "[info]"
	SymbolArrow   = "->"
)

// Spinner represents an animated spinner for long-running operations.
type Spinner struct {
	message  string
	frames   []string
	done     chan struct{}
	stopOnce sync.Once
}

// NewSpinner creates a new spinner with a message.
func NewSpinner(message string) *Spinner {
	return &Spinner{
		message: message,
		frames:  []string{"|", "/", "-", "\\"},
		done:    make(chan struct{}),
	}
}

// Start begins the spinner animation.
// The spinner will automatically stop after 10 minutes to prevent goroutine leaks.
func (s *Spinner) Start() {
	go func() {
		i := 0
		timeout := time.After(10 * time.Minute)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-s.done:
				return
			case <-timeout:
				// Auto-stop after timeout to prevent goroutine leak
				s.Stop()
				return
			case <-ticker.C:
				fmt.Printf("\r%s %s", DimStyle.Render(s.frames[i%len(s.frames)]), s.message)
				i++
			}
		}
	}()
}

// Stop ends the spinner animation.
func (s *Spinner) Stop() {
	s.stopOnce.Do(func() {
		close(s.done)
		fmt.Print("\r\033[K") // Clear line
	})
}

// StopWithSuccess ends the spinner and prints a success message.
func (s *Spinner) StopWithSuccess(message string) {
	s.Stop()
	Success("%s", message)
}

// StopWithError ends the spinner and prints an error message.
func (s *Spinner) StopWithError(message string) {
	s.Stop()
	Error("%s", message)
}

// Steps tracks progress through a multi-step operation.
type Steps struct {
	total   int
	current int
}

// NewSteps creates a new step tracker.
func NewSteps(total int) *Steps {
	return &Steps{total: total, current: 0}
}

// Next advances to the next step and prints the message.
func (s *Steps) Next(format string, args ...any) {
	s.current++
	msg := fmt.Sprintf(format, args...)
	prefix := DimStyle.Render(fmt.Sprintf("[%d/%d]", s.current, s.total))
	fmt.Printf("%s %s\n", prefix, InfoStyle.Render(msg))
}

// Done prints a completion message for the current step.
func (s *Steps) Done(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	prefix := DimStyle.Render(fmt.Sprintf("[%d/%d]", s.current, s.total))
	fmt.Printf("%s %s\n", prefix, SuccessStyle.Render(msg))
}

// Skip prints a skip message for the current step.
func (s *Steps) Skip(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	prefix := DimStyle.Render(fmt.Sprintf("[%d/%d]", s.current, s.total))
	fmt.Printf("%s %s\n", prefix, DimStyle.Render(msg))
}

// Success prints a success message with green color.
func Success(format string, args ...any) {
	fmt.Println(SuccessStyle.Render(fmt.Sprintf(format, args...)))
}

// Error prints an error message with red color to stderr.
func Error(format string, args ...any) {
	fmt.Fprintln(os.Stderr, ErrorStyle.Render(fmt.Sprintf(format, args...)))
}

// Warn prints a warning message with yellow color.
func Warn(format string, args ...any) {
	fmt.Println(WarnStyle.Render(fmt.Sprintf(format, args...)))
}

// Info prints an info message with blue color.
func Info(format string, args ...any) {
	fmt.Println(InfoStyle.Render(fmt.Sprintf(format, args...)))
}

// Dim prints a dimmed message with gray color.
func Dim(format string, args ...any) {
	fmt.Println(DimStyle.Render(fmt.Sprintf(format, args...)))
}

// Bold prints a bold message.
func Bold(format string, args ...any) {
	fmt.Println(BoldStyle.Render(fmt.Sprintf(format, args...)))
}

// Code prints text styled as code (cyan).
func Code(format string, args ...any) {
	fmt.Println(CodeStyle.Render(fmt.Sprintf(format, args...)))
}

// Print prints a plain message (for when you need a newline without color).
func Print(format string, args ...any) {
	fmt.Println(fmt.Sprintf(format, args...))
}

// Blank prints an empty line.
func Blank() {
	fmt.Println()
}

// VerboseLog prints a message only if verbose mode is enabled.
func VerboseLog(format string, args ...any) {
	if Verbose {
		fmt.Println(DimStyle.Render(fmt.Sprintf(format, args...)))
	}
}

// Indent returns a string with the given indentation level.
func Indent(level int, format string, args ...any) string {
	indent := strings.Repeat("  ", level)
	return indent + fmt.Sprintf(format, args...)
}

// IndentedSuccess prints an indented success message.
func IndentedSuccess(level int, format string, args ...any) {
	fmt.Println(SuccessStyle.Render(Indent(level, format, args...)))
}

// IndentedError prints an indented error message.
func IndentedError(level int, format string, args ...any) {
	fmt.Fprintln(os.Stderr, ErrorStyle.Render(Indent(level, format, args...)))
}

// IndentedWarn prints an indented warning message.
func IndentedWarn(level int, format string, args ...any) {
	fmt.Println(WarnStyle.Render(Indent(level, format, args...)))
}

// IndentedInfo prints an indented info message.
func IndentedInfo(level int, format string, args ...any) {
	fmt.Println(InfoStyle.Render(Indent(level, format, args...)))
}

// IndentedDim prints an indented dimmed message.
func IndentedDim(level int, format string, args ...any) {
	fmt.Println(DimStyle.Render(Indent(level, format, args...)))
}

// =============================================================================
// Thread-safe output functions for parallel operations
// =============================================================================

// SafeIndentedDim prints an indented dimmed message (thread-safe).
func SafeIndentedDim(level int, format string, args ...any) {
	printMu.Lock()
	defer printMu.Unlock()
	fmt.Println(DimStyle.Render(Indent(level, format, args...)))
}

// SafeError prints an error message (thread-safe).
func SafeError(format string, args ...any) {
	printMu.Lock()
	defer printMu.Unlock()
	fmt.Fprintln(os.Stderr, ErrorStyle.Render(fmt.Sprintf(format, args...)))
}

// SafeWarn prints a warning message (thread-safe).
func SafeWarn(format string, args ...any) {
	printMu.Lock()
	defer printMu.Unlock()
	fmt.Println(WarnStyle.Render(fmt.Sprintf(format, args...)))
}

// StatusColor returns a colored status string.
func StatusColor(status string) string {
	switch status {
	case "running", "valid":
		return SuccessStyle.Render(status)
	case "stopped", "auto":
		return DimStyle.Render(status)
	case "broken", "expired", "missing":
		return ErrorStyle.Render(status)
	case "expiring":
		return WarnStyle.Render(status)
	default:
		if len(status) > 0 && status[0] == 'p' { // partial
			return WarnStyle.Render(status)
		}
		return status
	}
}

// TypeColor returns a colored type string.
func TypeColor(isLocal bool) string {
	if isLocal {
		return CyanStyle.Render("local")
	}
	return SuccessStyle.Render("production")
}

// Highlight returns text with cyan highlighting.
func Highlight(s string) string {
	return CyanStyle.Render(s)
}

// SuccessText returns green colored text without newline.
func SuccessText(s string) string {
	return SuccessStyle.Render(s)
}

// ErrorText returns red colored text without newline.
func ErrorText(s string) string {
	return ErrorStyle.Render(s)
}

// WarnText returns yellow colored text without newline.
func WarnText(s string) string {
	return WarnStyle.Render(s)
}

// InfoText returns blue colored text without newline.
func InfoText(s string) string {
	return InfoStyle.Render(s)
}

// DimText returns gray colored text without newline.
func DimText(s string) string {
	return DimStyle.Render(s)
}

// PrintTable prints a formatted table with colored headers.
func PrintTable(headers []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}

	// Calculate column widths (accounting for ANSI escape codes)
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			// Strip ANSI codes for width calculation
			plainCell := stripAnsi(cell)
			if i < len(widths) && len(plainCell) > widths[i] {
				widths[i] = len(plainCell)
			}
		}
	}

	// Print header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(white)
	for i, h := range headers {
		fmt.Print(headerStyle.Render(fmt.Sprintf("%-*s", widths[i], h)))
		if i < len(headers)-1 {
			fmt.Print("  ")
		}
	}
	fmt.Println()

	// Print rows
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				// Pad accounting for ANSI codes
				plainCell := stripAnsi(cell)
				padding := widths[i] - len(plainCell)
				fmt.Print(cell + strings.Repeat(" ", padding))
				if i < len(row)-1 {
					fmt.Print("  ")
				}
			}
		}
		fmt.Println()
	}
}

// stripAnsi removes ANSI escape codes from a string for width calculation.
func stripAnsi(s string) string {
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}
