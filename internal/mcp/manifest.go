package mcp

import (
	"context"
	"fmt"
	"sort"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolDoc is one row of the public MCP tool manifest: a tool's name, the
// activation tier it belongs to (core/read/write), and its description as the
// server advertises it. gen-readme renders these into the README so the
// documented tool table can never drift from the registered surface.
type ToolDoc struct {
	Name        string
	Tier        string // "core" | "read" | "write"
	Title       string // human title from the tool's annotations, if any
	Description string
}

// tierOf maps every advertised tool name to its activation tier. Built from the
// same name lists srv_activate registers, so it stays in sync by construction.
func tierOf() map[string]string {
	m := make(map[string]string, len(coreToolNames)+len(readToolNames)+len(writeToolNames))
	for _, n := range coreToolNames {
		m[n] = "core"
	}
	for _, n := range readToolNames {
		m[n] = "read"
	}
	for _, n := range writeToolNames {
		m[n] = "write"
	}
	return m
}

// tierRank orders tiers for stable, logical rendering: core first, then the
// tiers in activation order.
var tierRank = map[string]int{"core": 0, "read": 1, "write": 2}

// ToolManifest returns every tool the MCP server can advertise (the full
// surface, as if both tiers were activated), tagged with its tier and sorted
// by tier then name. It spins up the real server over an in-memory transport
// and lists the tools, so the manifest reflects exactly what clients see — no
// second source of truth to maintain.
func ToolManifest(ctx context.Context) ([]ToolDoc, error) {
	srv := newServer()
	// Register the on-demand tiers up front so ListTools returns the whole
	// surface, not just the core gateway.
	registerReadTools(srv)
	registerDiagTools(srv)
	registerWriteTools(srv)

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("server connect: %w", err)
	}
	defer func() { _ = serverSession.Close() }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "gen-readme", Version: "manifest"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("client connect: %w", err)
	}
	defer func() { _ = clientSession.Close() }()

	res, err := clientSession.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	tier := tierOf()
	docs := make([]ToolDoc, 0, len(res.Tools))
	for _, t := range res.Tools {
		d := ToolDoc{
			Name:        t.Name,
			Tier:        tier[t.Name],
			Description: t.Description,
		}
		if t.Annotations != nil {
			d.Title = t.Annotations.Title
		}
		docs = append(docs, d)
	}

	sort.SliceStable(docs, func(i, j int) bool {
		if ri, rj := tierRank[docs[i].Tier], tierRank[docs[j].Tier]; ri != rj {
			return ri < rj
		}
		return docs[i].Name < docs[j].Name
	})
	return docs, nil
}
