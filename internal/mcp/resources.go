package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
)

// Resource URIs exposed by the srv MCP server.
const (
	resourceUserConfig = "srv://config/user"
	resourcePaths      = "srv://paths"
)

// registerResources binds read-only MCP resources. Resources are passive data
// an agent can pull into context without invoking a tool — here the live user
// config (secrets redacted) and the on-disk path layout. Mirrors treeman's
// treeman://config/* resources.
func registerResources(srv *mcpsdk.Server) {
	srv.AddResource(&mcpsdk.Resource{
		URI:         resourceUserConfig,
		Name:        "srv user config",
		Description: "The live ~/.config/srv/config.yml as JSON, with any secrets redacted. Reflects what srv actually loaded.",
		MIMEType:    "application/json",
	}, userConfigResource)

	srv.AddResource(&mcpsdk.Resource{
		URI:         resourcePaths,
		Name:        "srv paths",
		Description: "The on-disk paths srv reads and writes (config root, sites dir, proxies dir, traefik conf dir) as JSON.",
		MIMEType:    "application/json",
	}, pathsResource)
}

func userConfigResource(_ context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	userCfg, err := cfg.LoadUserConfig()
	if err != nil {
		return nil, fmt.Errorf("load user config: %w", err)
	}
	data, err := json.MarshalIndent(redactedJSONMap(userCfg), "", "  ")
	if err != nil {
		return nil, err
	}
	return resourceJSON(req.Params.URI, data), nil
}

func pathsResource(_ context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	data, err := json.MarshalIndent(pathsOut{
		ConfigRoot:     cfg.Root,
		SitesDir:       filepath.Join(cfg.Root, "sites"),
		ProxiesDir:     filepath.Join(cfg.Root, "proxies"),
		TraefikDir:     cfg.TraefikDir,
		TraefikConfDir: cfg.TraefikConfDir(),
		UserConfigFile: filepath.Join(cfg.Root, constants.UserConfigFile),
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return resourceJSON(req.Params.URI, data), nil
}

// resourceJSON wraps raw JSON bytes in a single-content ReadResourceResult.
func resourceJSON(uri string, data []byte) *mcpsdk.ReadResourceResult {
	return &mcpsdk.ReadResourceResult{
		Contents: []*mcpsdk.ResourceContents{
			{URI: uri, MIMEType: "application/json", Text: string(data)},
		},
	}
}
