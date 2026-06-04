package mcp

import "regexp"

// redactedPlaceholder replaces any value scrubbed from MCP tool output.
const redactedPlaceholder = "[REDACTED]"

// sensitiveKeyPattern matches map keys whose values must never be returned to
// an LLM: passwords, tokens, ACME/private-key material, generic secrets.
var sensitiveKeyPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|private[_-]?key|credential|http_pass)`)

// pemBlockPattern matches a PEM private-key block embedded in a string value.
var pemBlockPattern = regexp.MustCompile(`(?s)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`)

// inlineSecretPattern matches inline `key=value` / `key: value` secrets in a
// free-text string (e.g. an env line carrying a password).
var inlineSecretPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|http_pass)([=:]\s*)\S+`)

// redactValue recursively scrubs secrets from a JSON-decoded value before it is
// returned to the model. Maps are walked key-by-key (a sensitive key has its
// whole value replaced); strings are scanned for PEM blocks and inline secrets;
// slices are walked element-wise. The intent mirrors treeman's redactMap: TLS
// private keys and dnsmasq HTTP credentials must not leak through a tool result.
func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if sensitiveKeyPattern.MatchString(k) {
				out[k] = redactedPlaceholder
				continue
			}
			out[k] = redactValue(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = redactValue(val)
		}
		return out
	case string:
		return redactString(x)
	default:
		return v
	}
}

// redactString scrubs PEM private keys and inline secrets from a single string.
func redactString(s string) string {
	if pemBlockPattern.MatchString(s) {
		s = pemBlockPattern.ReplaceAllString(s, redactedPlaceholder)
	}
	if inlineSecretPattern.MatchString(s) {
		s = inlineSecretPattern.ReplaceAllString(s, "${1}${2}"+redactedPlaceholder)
	}
	return s
}

// redactMap is the map-typed convenience wrapper used by tools that return a
// metadata map. Returns nil unchanged so callers can pass toJSONMap output
// straight through.
func redactMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out, _ := redactValue(m).(map[string]any)
	return out
}

// redactedJSONMap marshals v to a map and scrubs secrets in one step — the safe
// replacement for toJSONMap on any payload that could carry credentials.
func redactedJSONMap(v any) map[string]any {
	return redactMap(toJSONMap(v))
}
