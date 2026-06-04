package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// confirmDestructive gates a destructive tool behind an MCP elicitation prompt.
// Semantics, matching treeman:
//   - dryRun or ack short-circuits to (true, "") so callers can preview or
//     pre-authorize without a prompt.
//   - if the client does not support elicitation (or it errors), it falls
//     through to (true, "") — refusing would break non-interactive agents that
//     cannot answer the question at all.
//   - an explicit decline/cancel returns (false, reason) so the tool can abort.
//
// The pattern: clients that DO support elicitation (Claude Desktop, etc.) get a
// confirmation pop-up before a site/proxy/redirect is removed; clients that
// don't are unchanged. Agents that want to skip the prompt pass ack=true.
func confirmDestructive(ctx context.Context, req *mcpsdk.CallToolRequest, dryRun, ack bool, message string) (bool, string) {
	if dryRun || ack {
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
