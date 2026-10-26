# AI integration (MCP)

`srv mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io)
server so AI agents can drive srv the way the CLI does: sites, proxies,
redirects, routes, networks and volumes.

## Connecting a client

Claude Code, no file editing:

```sh
claude mcp add srv -- srv mcp          # current project
claude mcp add -s user srv -- srv mcp  # all projects
```

Most other clients share the same `mcpServers` schema:

```json
{ "mcpServers": { "srv": { "command": "srv", "args": ["mcp"] } } }
```

Paste it into Claude Desktop's `claude_desktop_config.json`, Cursor's
`~/.cursor/mcp.json`, Windsurf's `mcp_config.json`, or the MCP panel of
Cline or Roo Code. VS Code (Copilot agent mode) uses a `servers` key:

```sh
code --add-mcp '{"name":"srv","command":"srv","args":["mcp"]}'
```

If the client does not inherit your shell `PATH`, use the absolute path
from `which srv`.

## Lazy-loaded tools

At startup the server advertises only `version` and `srv_activate`, so a
session that never uses srv spends no context on it.
`srv_activate(group="read")` unlocks inspection and diagnostics;
`srv_activate(group="write")` (the default) also unlocks every mutating
tool. Activation lasts for the session, and destructive tools still ask
for confirmation whatever the tier. Every tool is listed in the
[MCP tool reference](mcp-tools.md).

## Transports

- **stdio** (default): the client launches one `srv mcp` process per
  client. Nothing to host or keep running.
- **Streamable HTTP** (`--http`): one long-running server shared by every
  MCP client on the host.

```sh
srv mcp --http                 # listens on 127.0.0.1:8765/mcp
srv mcp --http=0.0.0.0:9000    # bind elsewhere; --http-path=/foo to remap
```

Each HTTP session keeps its own activation state. Relative paths in
`add_site` and `add_volume` resolve against the client's MCP roots or an
`X-Repo-Root` header. Mutating calls are serialized across clients, and
each is bounded by `--tool-timeout` (default 10m).

!!! warning "No authentication"
    The HTTP endpoint binds loopback with no auth. It mutates a privileged
    Traefik edge, so it trusts local processes only. Put it behind a
    reverse proxy with TLS and authentication before binding it off-host;
    browser clients on another origin need `--trusted-origin`.
