// Package ops is the single layer every surface goes through to read, validate
// and change srv's user configuration.
//
// Before it existed, config.yml had four independent entrypoints: the engine
// resolver read container_engine and quietly fell back on a typo, the dnsmasq
// writer read upstream_dns and interpolated it into a directive unvalidated,
// the MCP resource marshalled the struct with Go field names rather than the
// keys the file actually uses, and config.SaveUserConfig would write anything
// it was handed. Each one validated a different amount — which is to say, three
// of them validated nothing.
//
// So: one loader, one validator, one read-modify-write, one JSON projection.
// A value is checked once, here, at the point it enters srv — not at each of
// the places it is later interpolated into a config file.
package ops

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/engine"
)

// UserConfig loads config.yml. A file that fails validation is still returned
// alongside the error so a caller that must keep working (the engine resolver,
// the dnsmasq writer) can fall back to defaults while `srv doctor` reports the
// problem.
func UserConfig() (*config.UserConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		return &config.UserConfig{}, err
	}
	userCfg, err := cfg.LoadUserConfig()
	if err != nil {
		return &config.UserConfig{}, err
	}
	return userCfg, ValidateUserConfig(userCfg)
}

// UpdateUserConfig applies mutate to the current config and writes it back,
// refusing to persist a result that does not validate. This is the only
// supported way to change config.yml: a caller that marshals its own struct
// bypasses every check below.
func UpdateUserConfig(mutate func(*config.UserConfig) error) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// Load directly rather than through UserConfig(): an already-invalid file
	// must not block the edit that fixes it.
	userCfg, err := cfg.LoadUserConfig()
	if err != nil {
		return err
	}
	if err := mutate(userCfg); err != nil {
		return err
	}
	if err := ValidateUserConfig(userCfg); err != nil {
		return fmt.Errorf("refusing to write an invalid config: %w", err)
	}
	return cfg.SaveUserConfig(userCfg)
}

// ValidateUserConfig checks every field of config.yml. It reports all problems
// at once rather than the first, because a hand-edited file often has more than
// one and fixing them one round-trip at a time is miserable.
func ValidateUserConfig(c *config.UserConfig) error {
	if c == nil {
		return nil
	}
	var problems []error

	if err := engine.Validate(c.ContainerEngine); err != nil {
		problems = append(problems, err)
	}
	for _, s := range c.UpstreamDNS {
		if err := validateUpstreamDNS(s); err != nil {
			problems = append(problems, err)
		}
	}
	for _, p := range c.ParkedPaths {
		if err := validateParkedPath(p); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

// validateUpstreamDNS checks one `upstream_dns` entry.
//
// These are interpolated straight into dnsmasq.conf as `server=<value>`, one
// per line, so the value is trusted input to a config file srv generates: a
// newline injects arbitrary dnsmasq directives, and anything dnsmasq cannot
// parse stops it from starting at all, taking every local domain with it.
// dnsmasq's own grammar for the useful cases is an address, optionally
// #port — that is what is accepted here.
func validateUpstreamDNS(s string) error {
	if s == "" {
		return errors.New("upstream_dns: empty entry")
	}
	host, port, hasPort := strings.Cut(s, "#")
	if net.ParseIP(host) == nil {
		return fmt.Errorf("upstream_dns %q: %q is not an IP address", s, host)
	}
	if hasPort {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("upstream_dns %q: %q is not a port", s, port)
		}
	}
	return nil
}

// validateParkedPath checks one `parked_paths` entry. A relative path would be
// resolved against whatever directory srv happened to be started in, which is
// not something a config file can mean.
func validateParkedPath(p string) error {
	if p == "" {
		return errors.New("parked_paths: empty entry")
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("parked_paths %q: must be an absolute path", p)
	}
	return nil
}

// UserConfigJSON projects config.yml into the shape agents and scripts see.
//
// The keys are the yaml keys — the ones in the file the user edits and in the
// published JSON schema. Marshalling config.UserConfig directly does not give
// that: it carries only yaml tags, so encoding/json falls back to Go field
// names and an agent reading the resource would write back keys srv does not
// recognise.
func UserConfigJSON() (map[string]any, error) {
	userCfg, err := UserConfig()
	out := map[string]any{
		"container_engine": userCfg.ContainerEngine,
		"parked_paths":     nonNil(userCfg.ParkedPaths),
		"upstream_dns":     nonNil(userCfg.UpstreamDNS),
	}
	if err != nil {
		// Surface the problem in-band: a client reading this resource is
		// exactly who needs to know the file is not being honoured.
		out["invalid"] = err.Error()
	}
	return out, nil
}

// nonNil renders an absent list as [] rather than null, so a consumer can
// append to it without a nil check.
func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// ParkedPaths returns the directories `srv park` watches, empty rather than
// nil so a caller can range over it unguarded.
func ParkedPaths() ([]string, error) {
	userCfg, err := UserConfig()
	if err != nil {
		return nil, err
	}
	return nonNil(userCfg.ParkedPaths), nil
}

// SetParkedPaths replaces the parked-directory list.
//
// This lives here rather than on config.Config, where it used to: those
// accessors read and wrote config.yml directly, so they would have persisted a
// relative path that then resolved against whatever directory srv happened to
// be started in. They had no callers, which is the only reason it never bit.
func SetParkedPaths(paths []string) error {
	return UpdateUserConfig(func(c *config.UserConfig) error {
		c.ParkedPaths = paths
		return nil
	})
}
