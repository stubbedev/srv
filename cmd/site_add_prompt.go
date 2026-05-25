// Package cmd — site_add_prompt.go owns the flag-driven `srv add`
// configuration assembly (domain, service, profile) and the alias/limits
// normalisation helpers. Previously this file relied on huh prompts when
// flags were missing; it now requires every decision to come via a flag so
// `srv add` is scriptable end-to-end.
package cmd

import (
	"fmt"
	"strings"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

// promptForMissingConfig prompts user for any missing configuration
func promptForMissingConfig(setup *siteSetup) error {
	// Get service name (only for compose sites)
	if !setup.isStatic && !setup.isNode && !setup.isRuby && !setup.isPython && !setup.isDockerfile {
		if err := promptForService(setup); err != nil {
			return err
		}
	}

	// Get domain first (needed for site name)
	if err := promptForDomain(setup); err != nil {
		return err
	}

	// Get site name - use domain by default for uniqueness
	setup.siteName = addFlags.name
	if setup.siteName == "" {
		setup.siteName = site.SanitizeName(setup.domain)
	}

	// Check if site already exists
	if site.Exists(setup.siteName) && !addFlags.force {
		return fmt.Errorf("site '%s' already exists. Use --force to overwrite", setup.siteName)
	}

	// Determine if local - require --local flag explicitly, don't auto-detect from domain
	setup.isLocal = addFlags.local
	setup.wildcard = addFlags.wildcard
	if setup.wildcard && !setup.isLocal {
		return fmt.Errorf("--wildcard requires --local (Let's Encrypt cannot issue local wildcard certs)")
	}

	// Aliases: validate each, ensure no clash with the canonical domain, dedupe.
	if aliases, err := normalizeAliases(setup.domain, addFlags.aliases); err != nil {
		return err
	} else {
		setup.aliases = aliases
	}

	// Limits: collect any user-supplied overrides. Empty values are omitted.
	if l := limitsFromFlags(); l != nil {
		setup.limits = l
	}

	if addFlags.internalHTTP {
		setup.listeners = append(setup.listeners, constants.ListenerInternal)
	}

	// Static site options
	setup.spa = addFlags.spa
	setup.cache = addFlags.cache
	setup.cors = addFlags.cors

	// Node.js site: apply flag overrides on top of auto-detected values.
	if setup.isNode && setup.nodeInfo != nil {
		if addFlags.nodeVersion != "" {
			setup.nodeInfo.NodeVersion = addFlags.nodeVersion
		}
		// If the user explicitly set --port, use it; otherwise keep the detected port.
		if addFlags.port != constants.DefaultContainerPort {
			setup.nodeInfo.Port = addFlags.port
		}
	}

	return nil
}

// promptForService prompts for service selection if needed
func promptForService(setup *siteSetup) error {
	if setup.composePath == "" {
		return nil
	}

	services, err := site.GetServiceInfos(setup.composePath)
	if err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	if len(services) == 0 {
		return fmt.Errorf("no services found in compose file")
	}

	// Helper to set service info including discovered port
	setServiceInfo := func(svc site.ServiceInfo) error {
		if err := ValidateContainerName(svc.ContainerName); err != nil {
			return fmt.Errorf("compose container name: %w", err)
		}
		if err := ValidateContainerName(svc.ServiceName); err != nil {
			return fmt.Errorf("compose service name: %w", err)
		}
		setup.serviceName = svc.ContainerName
		setup.composeServiceName = svc.ServiceName
		// Use discovered port if user didn't explicitly set one
		if svc.Port > 0 && setup.port == constants.DefaultContainerPort {
			setup.port = svc.Port
			ui.Info("Auto-discovered port: %d", svc.Port)
		}
		return nil
	}

	var selectedService *site.ServiceInfo

	// If --service flag provided, find the matching service
	if addFlags.service != "" {
		for i, svc := range services {
			if svc.ContainerName == addFlags.service || svc.ServiceName == addFlags.service {
				selectedService = &services[i]
				break
			}
		}
		if selectedService == nil {
			return fmt.Errorf("service '%s' not found in compose file", addFlags.service)
		}
	} else if len(services) == 1 {
		// Single service - use it automatically
		selectedService = &services[0]
	} else {
		// Multiple services and no --service flag: refuse to guess.
		labels := make([]string, len(services))
		for i, svc := range services {
			labels[i] = svc.ContainerName
			if svc.ServiceName != svc.ContainerName {
				labels[i] = fmt.Sprintf("%s (service: %s)", svc.ContainerName, svc.ServiceName)
			}
		}
		return fmt.Errorf("compose file declares %d services (%s); pass --service to pick one", len(services), strings.Join(labels, ", "))
	}

	// Set the service info
	if err := setServiceInfo(*selectedService); err != nil {
		return err
	}

	// Handle profile selection
	if len(selectedService.Profiles) == 1 {
		setup.profile = selectedService.Profiles[0]
	} else if len(selectedService.Profiles) > 1 {
		// Multiple profiles - prompt for selection
		if err := promptForProfile(setup, selectedService.Profiles); err != nil {
			return err
		}
	}

	return nil
}

// promptForProfile resolves the compose profile from --profile. When the
// selected service declares multiple profiles, --profile must be set and
// must match one of them.
func promptForProfile(setup *siteSetup, profiles []string) error {
	flag := addFlags.profile
	if flag == "" {
		return fmt.Errorf("compose service declares %d profiles (%s); pass --profile to pick one", len(profiles), strings.Join(profiles, ", "))
	}
	for _, p := range profiles {
		if p == flag {
			setup.profile = flag
			return nil
		}
	}
	return fmt.Errorf("--profile %q is not one of the service's profiles (%s)", flag, strings.Join(profiles, ", "))
}

// promptForDomain pulls the domain from --domain. Missing is a hard error;
// the command level already enforces --domain at PreRun, so this is a belt
// for callers that bypass the flag.
func promptForDomain(setup *siteSetup) error {
	setup.domain = addFlags.domain
	if setup.domain == "" {
		return ui.UsageError("srv add PATH --domain DOMAIN", "--domain is required")
	}
	return nil
}

// validateSiteInputs validates all site inputs
func validateSiteInputs(setup *siteSetup) error {
	// Validate site name if explicitly provided
	if addFlags.name != "" {
		if err := ValidateSiteName(setup.siteName); err != nil {
			return err
		}
	}

	// Validate domain if provided via flag
	if addFlags.domain != "" {
		if err := ValidateDomain(setup.domain); err != nil {
			return err
		}
	}

	// Validate port
	if err := ValidatePort(setup.port); err != nil {
		return err
	}

	return nil
}

// normalizeAliases validates the supplied alias list, lowercases entries, drops
// duplicates, and rejects any alias equal to the canonical domain.
func normalizeAliases(canonical string, aliases []string) ([]string, error) {
	canonical = strings.ToLower(strings.TrimSpace(canonical))
	seen := map[string]bool{canonical: true}
	out := make([]string, 0, len(aliases))
	for _, raw := range aliases {
		a := strings.ToLower(strings.TrimSpace(raw))
		if a == "" {
			continue
		}
		if err := ValidateDomain(a); err != nil {
			return nil, fmt.Errorf("invalid alias %q: %w", raw, err)
		}
		if seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out, nil
}

// limitsFromFlags returns a *site.Limits populated from any non-empty
// --max-body / --*-timeout flags, or nil if none were supplied.
func limitsFromFlags() *site.Limits {
	if addFlags.maxBody == "" && addFlags.readTimeout == "" && addFlags.sendTimeout == "" && addFlags.connectTimeout == "" {
		return nil
	}
	return &site.Limits{
		MaxBody:        addFlags.maxBody,
		ReadTimeout:    addFlags.readTimeout,
		SendTimeout:    addFlags.sendTimeout,
		ConnectTimeout: addFlags.connectTimeout,
	}
}
