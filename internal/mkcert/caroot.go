package mkcert

import (
	"os"
	"path/filepath"
	"runtime"
)

// CAROOT returns the directory holding the local CA (rootCA.pem and
// rootCA-key.pem). The $CAROOT environment variable overrides the platform
// default, allowing multiple local CAs in parallel — identical to the
// standalone mkcert tool.
func CAROOT() string { return getCAROOT() }

// CARootCertPath returns the path of the local CA certificate file inside
// CAROOT, so callers outside this package can trust the exact CA srv issues
// certificates from without duplicating its filename.
func CARootCertPath() string { return filepath.Join(CAROOT(), rootName) }

func getCAROOT() string {
	if env := os.Getenv("CAROOT"); env != "" {
		return env
	}

	var dir string
	switch {
	case runtime.GOOS == "windows":
		dir = os.Getenv("LocalAppData")
	case os.Getenv("XDG_DATA_HOME") != "":
		dir = os.Getenv("XDG_DATA_HOME")
	case runtime.GOOS == "darwin":
		dir = os.Getenv("HOME")
		if dir == "" {
			return ""
		}
		dir = filepath.Join(dir, "Library", "Application Support")
	default: // Unix
		dir = os.Getenv("HOME")
		if dir == "" {
			return ""
		}
		dir = filepath.Join(dir, ".local", "share")
	}
	return filepath.Join(dir, "mkcert")
}
