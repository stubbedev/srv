package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/stubbedev/srv/internal/mcp"
)

func renderMCPTools() (string, error) {
	docs, err := mcp.ToolManifest(context.Background())
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# MCP tools\n\n")
	writeGeneratedNote(&b, "the live MCP server")
	b.WriteString("Every tool `srv mcp` exposes, by tier. Only the `core` tools are\n")
	b.WriteString("advertised until `srv_activate` unlocks a tier; see\n")
	b.WriteString("[AI integration](mcp.md).\n\n")
	b.WriteString("| Tier | Tool | Description |\n|---|---|---|\n")
	for _, d := range docs {
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", d.Tier, d.Name, firstSentence(d.Description))
	}
	return b.String(), nil
}

// firstSentence trims a (often agent-verbose) tool description to its first
// sentence so the table stays scannable, then sanitizes it for a cell.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i+1]
	}
	return oneLine(s)
}
