package mkcert

// Version is the revision of the vendored upstream mkcert implementation.
const Version = "v1.4.4-1-g1c1dc4e"

// InstallResult describes the outcome of installing the local CA into the
// platform trust stores. It carries the same signal the standalone `mkcert
// -install` used to print as multi-line console output, so srv can render a
// single clean message per outcome.
type InstallResult struct {
	CARootPath         string // Path to rootCA.pem (always set on a successful load)
	SystemTrustOK      bool   // CA installed (or already present) in the OS trust store
	BrowserTrustOK     bool   // CA installed in the NSS DB (Firefox/Chrome)
	SystemUnsupported  bool   // System trust store install not supported on this platform
	BrowserUnavailable bool   // NSS support unavailable on this platform
	CertutilMissing    bool   // certutil binary not found, needed for browser trust
	NewCA              bool   // A fresh local CA was created during this run
	SudoDenied         bool   // sudo password prompt failed or was refused; CA install aborted
	RawOutput          string // Human-readable log of what the engine did (for debugging)
}

// Engine performs the local-CA operations. The production implementation
// drives the host system (files, trust stores, sudo); tests swap in fakes via
// SwapEngine.
type Engine interface {
	// Available reports whether local TLS issuance can work on this host.
	Available() bool
	// Install creates the local CA if needed and enrolls it in the system and
	// NSS trust stores.
	Install() (InstallResult, error)
	// Uninstall removes the local CA from the trust stores (it does not
	// delete the CA files).
	Uninstall() error
	// IssueCert issues a certificate for domains (hostnames, wildcards, IPs)
	// at the given PEM paths.
	IssueCert(certPath, keyPath string, domains []string) error
}

// engine is the active Engine. Tests replace it via SwapEngine.
var engine Engine = systemEngine{}

// SwapEngine installs e and returns a function that restores the previous
// engine. Intended for use with t.Cleanup.
func SwapEngine(e Engine) func() {
	prev := engine
	engine = e
	return func() { engine = prev }
}

// SystemEngine returns the production engine backed by the host system.
// Test doubles embed it to keep real behavior for the operations they don't
// stub.
func SystemEngine() Engine { return systemEngine{} }

// Available reports whether local TLS issuance can work on this host: the
// implementation is compiled in, so the only requirement is a resolvable CA
// directory.
func Available() bool { return engine.Available() }

// Install creates the local CA if needed and enrolls it in the platform trust
// stores.
func Install() (InstallResult, error) { return engine.Install() }

// Uninstall removes the local CA from the platform trust stores without
// deleting the CA files.
func Uninstall() error { return engine.Uninstall() }

// IssueCert issues a certificate for domains at the given PEM paths,
// creating the local CA first if needed.
func IssueCert(certPath, keyPath string, domains []string) error {
	return engine.IssueCert(certPath, keyPath, domains)
}

// systemEngine is the production Engine.
type systemEngine struct{}

func (systemEngine) Available() bool { return CAROOT() != "" }

func (systemEngine) Install() (InstallResult, error) {
	ca, err := loadCA()
	if err != nil {
		return InstallResult{}, err
	}
	return ca.install()
}

func (systemEngine) Uninstall() error {
	ca, err := loadCA()
	if err != nil {
		return err
	}
	return ca.uninstall()
}

func (systemEngine) IssueCert(certPath, keyPath string, domains []string) error {
	hosts, err := validateHostnames(domains)
	if err != nil {
		return err
	}
	ca, err := loadCA()
	if err != nil {
		return err
	}
	return ca.makeCert(hosts, certPath, keyPath)
}
