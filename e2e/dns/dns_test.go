//go:build e2e

// End-to-end coverage for srv's embedded DNS server: boot the real `srv
// dnsd` against the generated zone files, register domains the way srv does,
// and resolve through the real UDP listener. No containers involved — the
// point is the wire protocol, the generated-file contract, and live reload.
package dns_test

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	miekg "github.com/miekg/dns"

	"github.com/stubbedev/srv/e2e/harness"
	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
	"github.com/stubbedev/srv/internal/traefik"
)

// startDNSD runs `srv dnsd` as a real subprocess on an ephemeral port
// (--port 0), pointed at the throwaway SRV_ROOT's generated zone files, and
// parses the bound address out of its output. Returns the address once the
// health-check name answers.
func startDNSD(t *testing.T, root string) string {
	t.Helper()

	if err := traefik.EnsureConfig(""); err != nil {
		t.Fatalf("EnsureConfig: %v", err)
	}
	// The registration path ends in a sudo'd host-cache flush that needs a
	// TTY this test does not have; the zone files on disk are the contract
	// dnsd reads, so the shell around the flush is stubbed like the unit
	// tests do. Everything downstream (dnsd, its watcher, the wire) is real.
	restore := shell.SwapDefault(shelltest.New(nil))
	if err := traefik.RegisterLocalDomain("exact.test", false); err != nil {
		restore()
		t.Fatalf("register exact.test: %v", err)
	}
	if err := traefik.RegisterLocalDomain("wild.test", true); err != nil {
		restore()
		t.Fatalf("register wild.test: %v", err)
	}
	restore()

	bin := harness.BuildSrv(t)
	cmd := exec.Command(bin, "dnsd", "--port", "0")
	cmd.Env = append(os.Environ(), constants.EnvSrvRoot+"="+root)
	var output syncBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start dnsd: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	})

	// dnsd logs "DNS server listening on <addr>" once bound; wait for that
	// line, then confirm on the wire.
	var addr string
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if a := output.lineContaining("listening on "); a != "" {
			addr = a
			break
		}
		if state := cmd.ProcessState; state != nil && state.Exited() {
			t.Fatalf("dnsd exited early:\n%s", output.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if addr == "" {
		t.Fatalf("dnsd never reported its address:\n%s", output.String())
	}

	wireDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(wireDeadline) {
		if queryA(t, addr, "srv-dns-check.test.") != "" {
			return addr
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("dnsd on %s never answered the health check\n%s", addr, output.String())
	return ""
}

// syncBuffer is a concurrency-safe bytes.Buffer: the dnsd subprocess writes
// from its own goroutine while the test polls it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// lineContaining returns the trimmed address that follows the first line
// carrying marker, e.g. "DNS server listening on 127.0.0.1:4123 (zones: …)".
func (b *syncBuffer) lineContaining(marker string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for line := range strings.SplitSeq(b.buf.String(), "\n") {
		if i := strings.Index(line, marker); i >= 0 {
			rest := strings.TrimSpace(line[i+len(marker):])
			if field := strings.Fields(rest); len(field) > 0 {
				return field[0]
			}
		}
	}
	return ""
}

// queryA resolves name against addr, returning the first A record's IP or
// "" when the answer is empty/absent. Failures return "" — assertions speak.
func queryA(t *testing.T, addr, name string) string {
	t.Helper()
	m := new(miekg.Msg)
	m.SetQuestion(miekg.Fqdn(name), miekg.TypeA)
	m.RecursionDesired = true
	client := &miekg.Client{Timeout: 2 * time.Second, Net: "udp"}
	resp, _, err := client.Exchange(m, addr)
	if err != nil || resp.Rcode != miekg.RcodeSuccess {
		return ""
	}
	for _, rr := range resp.Answer {
		if a, ok := rr.(*miekg.A); ok {
			return a.A.String()
		}
	}
	return ""
}

// waitForResolution polls until name resolves to 127.0.0.1 on addr.
func waitForResolution(t *testing.T, addr, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := queryA(t, addr, name); got == constants.LocalhostIP {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s never resolved to %s on %s", name, constants.LocalhostIP, addr)
}

func TestDNSDServesRegisteredDomains(t *testing.T) {
	root := harness.NewRoot(t)
	addr := startDNSD(t, root)

	// The health-check name is answered by the server itself.
	if got := queryA(t, addr, "srv-dns-check.test."); got != constants.LocalhostIP {
		t.Fatalf("health check = %q, want %s", got, constants.LocalhostIP)
	}
	// Exact entry from dnsmasq.hosts.
	if got := queryA(t, addr, "exact.test."); got != constants.LocalhostIP {
		t.Errorf("exact.test = %q, want %s", got, constants.LocalhostIP)
	}
	// Wildcard entry from dnsmasq.conf: apex, one level, deep.
	for _, name := range []string{"wild.test.", "a.wild.test.", "a.b.wild.test."} {
		if got := queryA(t, addr, name); got != constants.LocalhostIP {
			t.Errorf("%s = %q, want %s (wildcard covers every depth)", name, got, constants.LocalhostIP)
		}
	}
	// A name under an unregistered domain must not loop back to loopback.
	if got := queryA(t, addr, "unregistered.test."); got == constants.LocalhostIP {
		t.Error("unregistered.test resolved to loopback — zones are leaking")
	}
}

func TestDNSDReloadsLiveFromFileChange(t *testing.T) {
	root := harness.NewRoot(t)
	addr := startDNSD(t, root)

	if got := queryA(t, addr, "late.test."); got == constants.LocalhostIP {
		t.Fatal("late.test resolved before registration")
	}

	// Rewrite the hosts file the way srv does: atomic rename, full content.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	hostsPath := cfg.TraefikDir + "/" + constants.DnsmasqHostsDir + "/" + constants.DnsmasqHostsFile
	existing, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.TrimRight(string(existing), "\n") + "\n127.0.0.1 late.test\n"
	tmp := hostsPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, hostsPath); err != nil {
		t.Fatal(err)
	}

	// The running server watches the directory and re-reads on its own —
	// no signal, no restart. That is the contract site adds rely on.
	waitForResolution(t, addr, "late.test.", 10*time.Second)
}
