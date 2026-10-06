package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// confirmedKey marks a context whose destructive confirmation already ran
// (the middleware elicits before taking writeMu — an unanswered prompt while
// holding the lock stalled every other client's writes — and stamps the
// context so the handler's own confirmDestructive is a no-op).
type confirmedKey struct{}

func withConfirmed(ctx context.Context) context.Context {
	return context.WithValue(ctx, confirmedKey{}, true)
}

func ctxConfirmed(ctx context.Context) bool {
	b, _ := ctx.Value(confirmedKey{}).(bool)
	return b
}

// destructiveConfirmMessages builds each destructive tool's elicitation text
// from its raw JSON arguments. One table so the middleware can elicit before
// the lock while the handlers keep their exact per-tool wording.
var destructiveConfirmMessages = map[string]func(args map[string]any) string{
	"remove_site": func(a map[string]any) string {
		return fmt.Sprintf("Remove site %q? This stops its containers and deletes its config, cert, DNS, and metadata.", a["name"])
	},
	"remove_alias": func(a map[string]any) string {
		return fmt.Sprintf("Remove alias %q from site %q?", a["alias"], a["name"])
	},
	"remove_volume": func(a map[string]any) string {
		return fmt.Sprintf("Detach volume %q from site %q?", a["target"], a["name"])
	},
	"remove_route": func(a map[string]any) string {
		return fmt.Sprintf("Remove route %q from %q?", a["id"], a["target"])
	},
	"detach_network": func(a map[string]any) string {
		return fmt.Sprintf("Detach network %q from site %q?", a["network"], a["name"])
	},
	"remove_proxy": func(a map[string]any) string {
		return fmt.Sprintf("Remove proxy %q? This deletes its Traefik config, cert, and DNS registration.", a["name"])
	},
	"remove_redirect": func(a map[string]any) string {
		return fmt.Sprintf("Remove redirect %q?", a["name"])
	},
}

// preConfirm elicits the destructive confirmation for name before the write
// lock is taken. ok=false means the user declined and the call must not run.
func preConfirm(ctx context.Context, name string, req mcpsdk.Request) (context.Context, bool, string) {
	builder, destructive := destructiveConfirmMessages[name]
	if !destructive {
		return ctx, true, ""
	}
	args := map[string]any{}
	if p, ok := req.GetParams().(*mcpsdk.CallToolParamsRaw); ok {
		_ = json.Unmarshal(p.Arguments, &args)
	}
	// ack pre-authorizes and dry_run never mutates: the handler decides both.
	if v, _ := args["ack"].(bool); v {
		return ctx, true, ""
	}
	if v, _ := args["dry_run"].(bool); v {
		return ctx, true, ""
	}
	var sess *mcpsdk.ServerSession
	if ss, ok := req.GetSession().(*mcpsdk.ServerSession); ok {
		sess = ss
	}
	closer := &mcpsdk.CallToolRequest{Session: sess}
	if ok, reason := confirmDestructive(ctx, closer, false, false, builder(args)); !ok {
		return ctx, false, reason
	}
	return withConfirmed(ctx), true, ""
}

// confirmDestructive gates a destructive tool behind an MCP elicitation prompt.
// Semantics, matching treeman:
//   - dryRun or ack short-circuits to (true, "") so callers can preview or
//     pre-authorize without a prompt.
//   - a context the middleware already confirmed (elicitation answered
//     before the write lock) short-circuits the same way;
//   - if the client does not support elicitation (or it errors), it falls
//     through to (true, "") — refusing would break non-interactive agents that
//     cannot answer the question at all;
//   - an explicit decline/cancel returns (false, reason) so the tool can abort.
//
// The pattern: clients that DO support elicitation (Claude Desktop, etc.) get a
// confirmation pop-up before a site/proxy/redirect is removed; clients that
// don't are unchanged. Agents that want to skip the prompt pass ack=true.
func confirmDestructive(ctx context.Context, req *mcpsdk.CallToolRequest, dryRun, ack bool, message string) (bool, string) {
	if dryRun || ack || ctxConfirmed(ctx) {
		return true, ""
	}
	if req == nil || req.Session == nil {
		return true, ""
	}
	res, err := req.Session.Elicit(ctx, &mcpsdk.ElicitParams{
		Mode:    "confirmation",
		Message: message,
	})
	if err != nil || res == nil {
		return true, ""
	}
	switch res.Action {
	case "accept":
		return true, ""
	case "decline":
		return false, "user declined"
	case "cancel":
		return false, "user cancelled"
	default:
		return false, "user action: " + res.Action
	}
}
