// Package cmd — site_add_prompt.go owns the interactive `srv add` prompts
// (domain, service, profile) and the alias/limits flag-normalisation helpers
// they need.
package cmd

import (
	"fmt"
	"strings"

	"charm.land/huh/v2"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

// promptForMissingConfig prompts user for any missing configuration
func promptForMissingConfig(setup *siteSetup) error {
	// Get service name (only for compose sites)
	if !setup.isStatic && !setup.isPHP && !setup.isNode && !setup.isRuby && !setup.isPython && !setup.isDockerfile {
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

	// PHP site: apply flag overrides on top of auto-detected values.
	if setup.isPHP && setup.phpInfo != nil {
		if addFlags.phpVersion != "" {
			setup.phpInfo.PHPVersion = addFlags.phpVersion
		}
		if addFlags.documentRoot != "" {
			setup.phpInfo.DocumentRoot = addFlags.documentRoot
		}
		if addFlags.phpExtensions != "" {
			setup.phpInfo.Extensions = site.ParseExtensionOverrides(
				addFlags.phpExtensions,
				setup.phpInfo.Extensions,
			)
		}
	}

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
		// Multiple services - prompt for selection
		options := make([]huh.Option[int], len(services))
		for i, svc := range services {
			label := svc.ContainerName
			if svc.ServiceName != svc.ContainerName {
				label = fmt.Sprintf("%s (service: %s)", svc.ContainerName, svc.ServiceName)
			}
			if len(svc.Profiles) > 0 {
				label = fmt.Sprintf("%s [%s]", label, strings.Join(svc.Profiles, ","))
			}
			// Show discovered port in selection
			if svc.Port > 0 {
				label = fmt.Sprintf("%s (port: %d)", label, svc.Port)
			}
			options[i] = huh.NewOption(label, i)
		}

		var selectedIdx int
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[int]().
					Title("Select container").
					Description("Which container should Traefik route to?").
					Options(options...).
					Value(&selectedIdx),
			),
		)
		if err := form.Run(); err != nil {
			return err
		}
		selectedService = &services[selectedIdx]
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

// promptForProfile prompts the user to select a profile when multiple are available
func promptForProfile(setup *siteSetup, profiles []string) error {
	options := make([]huh.Option[string], len(profiles))
	for i, profile := range profiles {
		options[i] = huh.NewOption(profile, profile)
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select profile").
				Description("Which Docker Compose profile should be used?").
				Options(options...).
				Value(&selected),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	setup.profile = selected
	return nil
}

// promptForDomain prompts for domain input if not provided
func promptForDomain(setup *siteSetup) error {
	setup.domain = addFlags.domain
	if setup.domain != "" {
		return nil
	}

	var domain string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Domain").
				Description("Enter the domain for this site").
				Placeholder("example.com or myapp.test").
				Value(&domain).
				Validate(ValidateDomain),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	setup.domain = domain
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
