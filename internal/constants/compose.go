package constants

import "strings"

// ComposeProjectFor returns the per-site Docker Compose project name. Each site
// gets its own project so `docker compose up/down` on one site never treats
// another site's (or the metrics stack's) containers as orphans. Lowercased
// because Compose project names must be lowercase.
func ComposeProjectFor(siteName string) string {
	return "srv-site-" + strings.ToLower(siteName)
}
