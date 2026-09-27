// Package domain holds hostname matching shared by srv's embedded servers.
// The DNS server and the static file server both answer wildcard
// registrations — "example.test" covering every subdomain — and both must
// resolve a name in time proportional to its depth, not to the number of
// registered domains. One implementation keeps their semantics identical.
package domain

import "strings"

// MatchSuffix returns the value registered for name or its nearest parent
// domain: "a.b.example.test" probes "a.b.example.test", "b.example.test",
// "example.test", then "test". Keys and name must share one normalization
// (case, trailing dot); a trailing dot is carried through every probe, so
// FQDN keys match FQDN names. Matching is per label, never by raw string
// suffix: "napp.test" does not match a registration for "app.test".
//
// The cost is one map probe per label, independent of len(registrations).
func MatchSuffix[V any](registrations map[string]V, name string) (V, bool) {
	for name != "" && name != "." {
		if v, ok := registrations[name]; ok {
			return v, true
		}
		_, parent, found := strings.Cut(name, ".")
		if !found {
			break
		}
		name = parent
	}
	var zero V
	return zero, false
}

// MatchParent is MatchSuffix for strict subdomains: name itself is never
// matched, only its parents. "x.example.test" matches "example.test";
// "example.test" does not match itself.
func MatchParent[V any](registrations map[string]V, name string) (V, bool) {
	if _, parent, found := strings.Cut(name, "."); found {
		return MatchSuffix(registrations, parent)
	}
	var zero V
	return zero, false
}
