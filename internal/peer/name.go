package peer

import (
	"fmt"
	"strings"
)

const Zone = "home.arpa"

const maxNameLength = 63

type Name string

func ParseName(s string) (Name, error) {
	if s == "" {
		return "", fmt.Errorf("peer name %q: empty", s)
	}
	if len(s) > maxNameLength {
		return "", fmt.Errorf("peer name %q: longer than %d characters", s, maxNameLength)
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return "", fmt.Errorf("peer name %q: starts or ends with a hyphen", s)
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return "", fmt.Errorf("peer name %q: only lowercase letters, digits and hyphens are allowed", s)
		}
	}
	return Name(s), nil
}

func (n Name) FQDN() string {
	return string(n) + "." + Zone
}

func NameFromHost(host string) (Name, error) {
	return ParseName(strings.TrimSuffix(strings.TrimSuffix(host, "."), "."+Zone))
}

func (n *Name) UnmarshalText(text []byte) error {
	parsed, err := ParseName(string(text))
	if err != nil {
		return err
	}
	*n = parsed
	return nil
}
