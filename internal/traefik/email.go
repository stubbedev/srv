// Package traefik — email.go owns the Let's Encrypt account email lifecycle:
// reading the cached value from env.traefik, validating + persisting a new
// one, surfacing a clear error when neither is set. Local-only setups don't
// care about this value at all.
package traefik

import (
	"fmt"
	"net/mail"
	"os"
	"strings"

	"github.com/hashicorp/go-envparse"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
)

// GetEmail returns the stored Let's Encrypt email. The `provided` argument is
// the value of the caller-supplied flag (e.g. `srv install --email`); when
// non-empty it overrides any value already on disk and is persisted via
// SaveEmail. When no email is on disk and the caller didn't provide one, a
// clear error directs the user to pass --email.
//
// Production SSL via Let's Encrypt is the only feature that needs this email;
// local-only setups can ignore the error returned by callers that swallow it.
func GetEmail(provided string) (string, error) {
	provided = strings.TrimSpace(provided)
	if provided != "" {
		if _, err := mail.ParseAddress(provided); err != nil {
			return "", fmt.Errorf("invalid email %q: %w", provided, err)
		}
		if err := SaveEmail(provided); err != nil {
			return "", err
		}
		return provided, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	envPath := cfg.EnvTraefikPath()
	if file, err := os.Open(envPath); err == nil {
		defer func() { _ = file.Close() }()
		envMap, err := envparse.Parse(file)
		if err == nil {
			if email, ok := envMap[constants.EnvACMEEmail]; ok && email != "" {
				return email, nil
			}
		}
	}
	return "", fmt.Errorf("no Let's Encrypt email configured. Pass `srv install --email you@example.com` or set %s in %s for production SSL", constants.EnvACMEEmail, envPath)
}

// SaveEmail saves the Let's Encrypt email to env.traefik, preserving any
// other keys already present (e.g. DNS_HTTP_USER / DNS_HTTP_PASS).
func SaveEmail(email string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	envPath := cfg.EnvTraefikPath()
	envMap := readEnvFile(envPath)
	envMap[constants.EnvACMEEmail] = email
	return writeEnvFile(envPath, envMap)
}
