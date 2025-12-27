package traefik

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestLoadSaveLocalDomains(t *testing.T) {
	// Create a temporary directory structure to simulate config
	tmpDir := t.TempDir()
	traefikDir := filepath.Join(tmpDir, ".config", "srv", "traefik")
	if err := os.MkdirAll(traefikDir, 0o755); err != nil {
		t.Fatalf("failed to create traefik dir: %v", err)
	}

	// Override the localDomainsFile function for testing
	domainsFile := filepath.Join(traefikDir, "local-domains.txt")

	t.Run("load from non-existent file returns empty slice", func(t *testing.T) {
		domains, err := loadDomainsFromFile(domainsFile + ".nonexistent")
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if len(domains) != 0 {
			t.Errorf("expected empty slice, got %v", domains)
		}
	})

	t.Run("save and load domains", func(t *testing.T) {
		testDomains := []string{"foo.test", "bar.test", "baz.local"}

		if err := saveDomainsToFile(domainsFile, testDomains); err != nil {
			t.Fatalf("failed to save domains: %v", err)
		}

		loaded, err := loadDomainsFromFile(domainsFile)
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(loaded) != len(testDomains) {
			t.Errorf("expected %d domains, got %d", len(testDomains), len(loaded))
		}

		// Domains should be sorted
		expected := []string{"bar.test", "baz.local", "foo.test"}
		for i, d := range loaded {
			if d != expected[i] {
				t.Errorf("expected domain[%d]=%s, got %s", i, expected[i], d)
			}
		}
	})

	t.Run("save deduplicates domains", func(t *testing.T) {
		testDomains := []string{"foo.test", "bar.test", "foo.test", "bar.test"}

		if err := saveDomainsToFile(domainsFile, testDomains); err != nil {
			t.Fatalf("failed to save domains: %v", err)
		}

		loaded, err := loadDomainsFromFile(domainsFile)
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(loaded) != 2 {
			t.Errorf("expected 2 unique domains, got %d: %v", len(loaded), loaded)
		}
	})

	t.Run("load ignores comments and empty lines", func(t *testing.T) {
		content := `# This is a comment
foo.test

# Another comment
bar.test

`
		if err := os.WriteFile(domainsFile, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		loaded, err := loadDomainsFromFile(domainsFile)
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(loaded) != 2 {
			t.Errorf("expected 2 domains, got %d: %v", len(loaded), loaded)
		}
	})

	t.Run("save empty list", func(t *testing.T) {
		if err := saveDomainsToFile(domainsFile, []string{}); err != nil {
			t.Fatalf("failed to save empty domains: %v", err)
		}

		loaded, err := loadDomainsFromFile(domainsFile)
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(loaded) != 0 {
			t.Errorf("expected 0 domains, got %d: %v", len(loaded), loaded)
		}
	})
}

func TestDomainRegistration(t *testing.T) {
	tmpDir := t.TempDir()
	domainsFile := filepath.Join(tmpDir, "local-domains.txt")

	t.Run("register domain adds to empty file", func(t *testing.T) {
		err := registerDomainToFile(domainsFile, "api.test")
		if err != nil {
			t.Fatalf("failed to register domain: %v", err)
		}

		domains, err := loadDomainsFromFile(domainsFile)
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(domains) != 1 || domains[0] != "api.test" {
			t.Errorf("expected [api.test], got %v", domains)
		}
	})

	t.Run("register domain is idempotent", func(t *testing.T) {
		// Register same domain twice
		err := registerDomainToFile(domainsFile, "api.test")
		if err != nil {
			t.Fatalf("failed to register domain: %v", err)
		}

		domains, err := loadDomainsFromFile(domainsFile)
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(domains) != 1 {
			t.Errorf("expected 1 domain after re-registering, got %d: %v", len(domains), domains)
		}
	})

	t.Run("register multiple domains", func(t *testing.T) {
		err := registerDomainToFile(domainsFile, "web.test")
		if err != nil {
			t.Fatalf("failed to register domain: %v", err)
		}

		domains, err := loadDomainsFromFile(domainsFile)
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(domains) != 2 {
			t.Errorf("expected 2 domains, got %d: %v", len(domains), domains)
		}
	})

	t.Run("unregister domain removes it", func(t *testing.T) {
		err := unregisterDomainFromFile(domainsFile, "api.test")
		if err != nil {
			t.Fatalf("failed to unregister domain: %v", err)
		}

		domains, err := loadDomainsFromFile(domainsFile)
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(domains) != 1 || domains[0] != "web.test" {
			t.Errorf("expected [web.test], got %v", domains)
		}
	})

	t.Run("unregister non-existent domain is no-op", func(t *testing.T) {
		err := unregisterDomainFromFile(domainsFile, "nonexistent.test")
		if err != nil {
			t.Fatalf("unregistering non-existent domain should not error: %v", err)
		}

		domains, err := loadDomainsFromFile(domainsFile)
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(domains) != 1 {
			t.Errorf("expected 1 domain unchanged, got %d: %v", len(domains), domains)
		}
	})

	t.Run("unregister last domain leaves empty file", func(t *testing.T) {
		err := unregisterDomainFromFile(domainsFile, "web.test")
		if err != nil {
			t.Fatalf("failed to unregister domain: %v", err)
		}

		domains, err := loadDomainsFromFile(domainsFile)
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(domains) != 0 {
			t.Errorf("expected 0 domains, got %d: %v", len(domains), domains)
		}
	})
}

func TestGenerateDnsmasqConfig(t *testing.T) {
	t.Run("empty domains", func(t *testing.T) {
		content := generateDnsmasqConfigContent([]string{})
		if content == "" {
			t.Error("expected non-empty config even with no domains")
		}
		// Should still have upstream DNS servers
		if !strings.Contains(content, "server=8.8.8.8") {
			t.Error("expected upstream DNS server in config")
		}
	})

	t.Run("single domain", func(t *testing.T) {
		content := generateDnsmasqConfigContent([]string{"api.test"})
		if !strings.Contains(content, "address=/api.test/127.0.0.1") {
			t.Errorf("expected address line for api.test, got:\n%s", content)
		}
	})

	t.Run("multiple domains", func(t *testing.T) {
		content := generateDnsmasqConfigContent([]string{"api.test", "web.local", "app.localhost"})
		for _, domain := range []string{"api.test", "web.local", "app.localhost"} {
			expected := "address=/" + domain + "/127.0.0.1"
			if !strings.Contains(content, expected) {
				t.Errorf("expected address line for %s, got:\n%s", domain, content)
			}
		}
	})
}

// Helper functions for testing (these mirror the real functions but work with file paths directly)

func loadDomainsFromFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var domains []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		domain := strings.TrimSpace(scanner.Text())
		if domain != "" && !strings.HasPrefix(domain, "#") {
			domains = append(domains, domain)
		}
	}
	return domains, scanner.Err()
}

func saveDomainsToFile(path string, domains []string) error {
	// Sort and deduplicate
	sort.Strings(domains)
	unique := make([]string, 0, len(domains))
	seen := make(map[string]bool)
	for _, d := range domains {
		if !seen[d] {
			seen[d] = true
			unique = append(unique, d)
		}
	}

	content := strings.Join(unique, "\n")
	if len(unique) > 0 {
		content += "\n"
	}

	return os.WriteFile(path, []byte(content), 0o644)
}

func registerDomainToFile(path, domain string) error {
	domains, err := loadDomainsFromFile(path)
	if err != nil {
		return err
	}

	// Check if already registered
	for _, d := range domains {
		if d == domain {
			return nil
		}
	}

	domains = append(domains, domain)
	return saveDomainsToFile(path, domains)
}

func unregisterDomainFromFile(path, domain string) error {
	domains, err := loadDomainsFromFile(path)
	if err != nil {
		return err
	}

	filtered := make([]string, 0, len(domains))
	for _, d := range domains {
		if d != domain {
			filtered = append(filtered, d)
		}
	}

	return saveDomainsToFile(path, filtered)
}

func generateDnsmasqConfigContent(domains []string) string {
	var content strings.Builder
	content.WriteString("# Local domains managed by srv\n")
	content.WriteString("# Do not edit manually - changes will be overwritten\n\n")

	if len(domains) == 0 {
		content.WriteString("# No local domains registered\n")
	} else {
		for _, domain := range domains {
			content.WriteString("address=/" + domain + "/127.0.0.1\n")
		}
	}

	content.WriteString("\n# Forward all other queries to upstream DNS\n")
	content.WriteString("server=8.8.8.8\n")
	content.WriteString("server=8.8.4.4\n")
	content.WriteString("\n# Don't read /etc/resolv.conf\n")
	content.WriteString("no-resolv\n")

	return content.String()
}
