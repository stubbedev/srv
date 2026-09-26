package daemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	miekg "github.com/miekg/dns"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/dnsd"
)

// startStubUpstream answers every A query with ip, on an ephemeral loopback
// UDP port. The returned spec is "ip#port", ready for config.yml's
// upstream_dns.
func startStubUpstream(t *testing.T, ip string) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &miekg.Server{PacketConn: pc, Handler: miekg.HandlerFunc(func(w miekg.ResponseWriter, r *miekg.Msg) {
		resp := new(miekg.Msg)
		resp.SetReply(r)
		resp.Answer = append(resp.Answer, &miekg.A{
			Hdr: miekg.RR_Header{Name: r.Question[0].Name, Rrtype: miekg.TypeA, Class: miekg.ClassINET, Ttl: 10},
			A:   net.ParseIP(ip),
		})
		_ = w.WriteMsg(resp)
	})}
	go func() { _ = srv.ActivateAndServe() }()
	t.Cleanup(func() { _ = srv.ShutdownContext(context.Background()) })
	_, port, err := net.SplitHostPort(pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	return "127.0.0.1#" + port
}

// queryA asks addr for name's A record and returns the first answer's IP, or
// "" when the answer is empty.
func queryA(t *testing.T, addr, name string) string {
	t.Helper()
	m := new(miekg.Msg)
	m.SetQuestion(miekg.Fqdn(name), miekg.TypeA)
	m.RecursionDesired = true
	client := &miekg.Client{Timeout: 2 * time.Second, Net: "udp"}
	resp, _, err := client.Exchange(m, addr)
	if err != nil {
		t.Fatalf("query %s: %v", name, err)
	}
	for _, rr := range resp.Answer {
		if a, ok := rr.(*miekg.A); ok {
			return a.A.String()
		}
	}
	return ""
}

// bindTestDNSServer starts the embedded DNS server on an ephemeral port over
// empty fallback zone files and binds it to the daemon, mirroring
// startEmbeddedDNS's wiring.
func bindTestDNSServer(t *testing.T, d *Daemon) *dnsd.Server {
	t.Helper()
	dir := t.TempDir()
	confPath := filepath.Join(dir, "dnsmasq.conf")
	hostsPath := filepath.Join(dir, "dnsmasq.hosts")
	for _, p := range []string{confPath, hostsPath} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server, err := dnsd.New(constants.LocalhostIP, 0, confPath, hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	go func() { _ = server.Serve() }()
	d.dns.Store(server)
	return server
}

// seedDNSInputs creates the registry, a DNS-alias redirect, and config.yml
// under a fresh SRV_ROOT: one exact entry, one wildcard, one alias pointing
// at a resolvable name, and a stub upstream so forwarded queries stay
// hermetic.
func seedDNSInputs(t *testing.T, root, upstream string) {
	t.Helper()
	traefikDir := filepath.Join(root, "traefik")
	confDir := filepath.Join(traefikDir, "conf")
	for _, dir := range []string{traefikDir, confDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	registry := filepath.Join(traefikDir, constants.LocalDomainsFile)
	if err := os.WriteFile(registry, []byte("exact.test\n*.wild.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(confDir, constants.RedirectConfigPrefix+"alias"+constants.ExtYAML)
	body := "dns:\n  source: alias.test\n  target: localhost\n"
	if err := os.WriteFile(alias, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	configBody := "upstream_dns:\n  - " + upstream + "\n"
	if err := os.WriteFile(filepath.Join(root, constants.UserConfigFile), []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshDNSZonesServesStructuredInputs(t *testing.T) {
	root := setupSrvRoot(t)
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.cancel)

	upstream := startStubUpstream(t, "10.9.9.9")
	seedDNSInputs(t, root, upstream)
	bindTestDNSServer(t, d)
	d.refreshDNSZones()

	addr := d.dns.Load().Addr()
	for name, wantIP := range map[string]string{
		"exact.test":    "127.0.0.1", // registry, exact
		"wild.test":     "127.0.0.1", // registry wildcard, apex
		"sub.wild.test": "127.0.0.1", // registry wildcard, subdomain
		"alias.test":    "127.0.0.1", // redirect alias pinned to target's IP
		"unknown.test":  "10.9.9.9",  // forwarded to config.yml's upstream
	} {
		if got := queryA(t, addr, name); got != wantIP {
			t.Errorf("%s = %q, want %q", name, got, wantIP)
		}
	}
}

func TestDNSZoneWatcherAppliesRegistryChange(t *testing.T) {
	root := setupSrvRoot(t)
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.cancel)

	upstream := startStubUpstream(t, "10.9.9.9")
	seedDNSInputs(t, root, upstream)
	bindTestDNSServer(t, d)
	d.startDNSZoneSource()
	d.refreshDNSZones()

	addr := d.dns.Load().Addr()
	if got := queryA(t, addr, "late.test"); got != "10.9.9.9" {
		t.Fatalf("late.test = %q before registration, want the forwarded %q", got, "10.9.9.9")
	}

	registry := filepath.Join(root, "traefik", constants.LocalDomainsFile)
	if err := os.WriteFile(registry, []byte("exact.test\n*.wild.test\nlate.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := queryA(t, addr, "late.test"); got == "127.0.0.1" {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("late.test still %q 3s after the registry change; the zone source watcher did not apply it", queryA(t, addr, "late.test"))
}

func TestIsDNSInput(t *testing.T) {
	for path, want := range map[string]bool{
		filepath.Join("traefik", constants.LocalDomainsFile):                 true,
		filepath.Join("traefik", "conf", "redirect-alias"+constants.ExtYAML): true,
		filepath.Join("root", constants.UserConfigFile):                      true,
		filepath.Join("traefik", "conf", "site-app"+constants.ExtYAML):       false,
		filepath.Join("traefik", "conf", "redirect-http"+constants.ExtYAML):  true,
		filepath.Join("traefik", constants.DnsmasqConfFile):                  false,
		filepath.Join("traefik", constants.DnsmasqHostsDir, "srv-domains"):   false,
		filepath.Join("root", "daemon.log"):                                  false,
	} {
		if got := isDNSInput(path); got != want {
			t.Errorf("isDNSInput(%q) = %v, want %v", path, got, want)
		}
	}
}
