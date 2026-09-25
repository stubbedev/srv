// envfile.go parses and writes the srv-owned env.traefik file. srv writes the
// file itself (writeEnvFile: sorted plain KEY=VALUE lines, no exports, no
// shell interpolation), so the reader only needs to handle that shape plus
// what a hand edit might add: comments, blank lines, and quoted values.
package traefik

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// parseEnvFile reads KEY=VALUE lines into a map. Blank lines and # comments
// are skipped; values may be wrapped in single or double quotes, which are
// stripped. A line without "=" is a parse error rather than a silent skip.
func parseEnvFile(r io.Reader) (map[string]string, error) {
	env := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE, got %q", lineNo, line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNo)
		}
		env[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading env file: %w", err)
	}
	return env, nil
}
