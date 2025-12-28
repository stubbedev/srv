package cmd

import (
	"strings"
	"testing"
)

func TestValidateDomain(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		wantErr bool
		errMsg  string
	}{
		// Valid domains
		{"simple domain", "example.com", false, ""},
		{"subdomain", "sub.example.com", false, ""},
		{"deep subdomain", "a.b.c.example.com", false, ""},
		{"local domain", "myapp.test", false, ""},
		{"with hyphen", "my-app.example.com", false, ""},
		{"numeric subdomain", "123.example.com", false, ""},
		{"single label", "localhost", false, ""},

		// Invalid domains
		{"empty", "", true, "cannot be empty"},
		{"starts with hyphen", "-example.com", true, "invalid domain format"},
		{"ends with hyphen", "example-.com", true, "invalid domain format"},
		{"double dot", "example..com", true, "invalid domain format"},
		{"starts with dot", ".example.com", true, "invalid domain format"},
		{"ends with dot", "example.com.", true, "invalid domain format"},
		{"has space", "exam ple.com", true, "invalid domain format"},
		{"has underscore", "exam_ple.com", true, "invalid domain format"},
		{"special chars", "example@.com", true, "invalid domain format"},

		// Length limits (regex catches >63 char labels before length check)
		{"label too long", strings.Repeat("a", 64) + ".com", true, "invalid domain format"},
		{"max label length ok", strings.Repeat("a", 63) + ".com", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDomain(tt.domain)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDomain(%q) error = %v, wantErr %v", tt.domain, err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateDomain(%q) error = %q, want error containing %q", tt.domain, err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		name    string
		port    string
		wantErr bool
		errMsg  string
	}{
		// Valid ports
		{"port 80", "80", false, ""},
		{"port 443", "443", false, ""},
		{"port 8080", "8080", false, ""},
		{"port 1", "1", false, ""},
		{"port 65535", "65535", false, ""},
		{"port 3000", "3000", false, ""},

		// Invalid ports
		{"empty", "", true, "cannot be empty"},
		{"port 0", "0", true, "out of range"},
		{"port negative", "-1", true, "out of range"},
		{"port too high", "65536", true, "out of range"},
		{"port way too high", "100000", true, "out of range"},
		{"not a number", "abc", true, "must be a number"},
		{"float", "80.5", true, "must be a number"},
		{"with spaces", " 80 ", true, "must be a number"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePortString(tt.port)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePortString(%q) error = %v, wantErr %v", tt.port, err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidatePortString(%q) error = %q, want error containing %q", tt.port, err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidateSiteName(t *testing.T) {
	tests := []struct {
		name     string
		siteName string
		wantErr  bool
		errMsg   string
	}{
		// Valid site names
		{"simple", "mysite", false, ""},
		{"with hyphen", "my-site", false, ""},
		{"with underscore", "my_site", false, ""},
		{"with numbers", "site123", false, ""},
		{"mixed", "my-site_123", false, ""},
		{"uppercase", "MySite", false, ""},
		{"single char", "a", false, ""},

		// Invalid site names
		{"empty", "", true, "cannot be empty"},
		{"starts with hyphen", "-mysite", true, "invalid site name"},
		{"starts with underscore", "_mysite", true, "invalid site name"},
		{"starts with number", "123site", false, ""}, // Numbers at start are valid per regex
		{"has space", "my site", true, "invalid site name"},
		{"has dot", "my.site", true, "invalid site name"},
		{"has special char", "my@site", true, "invalid site name"},
		{"has slash", "my/site", true, "invalid site name"},

		// Length limits
		{"too long", strings.Repeat("a", 64), true, "too long"},
		{"max length ok", strings.Repeat("a", 63), false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSiteName(tt.siteName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSiteName(%q) error = %v, wantErr %v", tt.siteName, err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateSiteName(%q) error = %q, want error containing %q", tt.siteName, err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidateProxyURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		// Valid URLs
		{"http localhost", "http://localhost:8080", false, ""},
		{"https localhost", "https://localhost:8080", false, ""},
		{"http with domain", "http://example.com", false, ""},
		{"https with domain", "https://example.com", false, ""},
		{"http with path", "http://localhost:8080/api", false, ""},
		{"http with ip", "http://127.0.0.1:3000", false, ""},

		// Invalid URLs
		{"empty", "", true, "cannot be empty"},
		{"no scheme", "localhost:8080", true, "invalid URL scheme"},
		{"ftp scheme", "ftp://localhost", true, "invalid URL scheme"},
		{"ws scheme", "ws://localhost", true, "invalid URL scheme"},
		{"no host", "http://", true, "must include a host"},
		{"invalid port", "http://localhost:99999", true, "invalid port"},
		{"port zero", "http://localhost:0", true, "invalid port"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProxyURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProxyURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateProxyURL(%q) error = %q, want error containing %q", tt.url, err.Error(), tt.errMsg)
			}
		})
	}
}
