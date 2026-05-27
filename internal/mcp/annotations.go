// Package mcp serves srv's tool surface over the Model Context Protocol so
// AI agents can drive setup, configuration, and observability the same way
// a human would from the CLI. The server is started by `srv mcp` over stdio.
//
// The package is self-contained — it does not import cmd — so the MCP and
// CLI surfaces evolve independently. Tool implementations call the same
// internal packages (site, proxy, redirect, traefik, daemon, doctor) that
// the CLI uses; both surfaces share that orchestration layer.
package mcp

import (
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// boolPtr is needed because ToolAnnotations uses *bool for
// DestructiveHint and OpenWorldHint — the SDK distinguishes
// unset (nil → default) from explicitly-false.
func boolPtr(b bool) *bool { return &b }

// readOnlyAnno marks a tool as side-effect-free. openWorld=true means the
// tool reaches outside srv's own state (filesystem, docker daemon, network
// probes); false means it touches only in-process state + cached config.
func readOnlyAnno(title string, openWorld bool) *mcpsdk.ToolAnnotations {
	return &mcpsdk.ToolAnnotations{
		Title:         title,
		ReadOnlyHint:  true,
		OpenWorldHint: boolPtr(openWorld),
	}
}

// writeAnno marks a tool as state-mutating, with destructive + idempotent
// hints set by the caller. Use destructive=true for anything that can lose
// work (removing a site, dropping a proxy); false for additive writes
// (registering a new site/proxy). The MCP spec recommends clients prompt
// the user before invoking destructive tools.
func writeAnno(title string, destructive, idempotent, openWorld bool) *mcpsdk.ToolAnnotations {
	return &mcpsdk.ToolAnnotations{
		Title:           title,
		DestructiveHint: boolPtr(destructive),
		IdempotentHint:  idempotent,
		OpenWorldHint:   boolPtr(openWorld),
	}
}
