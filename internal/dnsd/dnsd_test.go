package dnsd

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	miekg "github.com/miekg/dns"

	"github.com/stubbedev/srv/internal/constants"
)

func startTestServer(t *testing.T, conf, hosts string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	confPath := filepath.Join(dir, "dnsmasq.conf")
	hostsPath := filepath.Join(dir, "dnsmasq.hosts")
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostsPath, []byte(hosts), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := New("127.0.0.1", 0, confPath, hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	go func() { _ = s.Serve() }()
	waitReady(t, s.Addr())
	return s, s.Addr()
}

func waitReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if PingAddr(addr) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server on %s never answered the health check", addr)
}

func queryA(t *testing.T, addr, name string) *miekg.Msg {
	t.Helper()
	m := new(miekg.Msg)
	m.SetQuestion(miekg.Fqdn(name), miekg.TypeA)
	m.RecursionDesired = true
	client := &miekg.Client{Timeout: 2 * time.Second, Net: "udp"}
	resp, _, err := client.Exchange(m, addr)
	if err != nil {
		t.Fatalf("query %s: %v", name, err)
	}
	return resp
}

func TestServeZones(t *testing.T) {
	conf := "# comment\n" +
		"address=/wild.test/127.0.0.1\n" +
		"address=/alias.test/10.1.2.3\n" +
		"server=1.1.1.1\n"
	hosts := "# reg\n127.0.0.1 exact.test\n"

	_, addr := startTestServer(t, conf, hosts)

	// Health check answered locally even with no zones.
	if resp := queryA(t, addr, CheckName); len(resp.Answer) != 1 {
		t.Errorf("health check: got %d answers, want 1", len(resp.Answer))
	}

	for name, wantIP := range map[string]string{
		"exact.test.":    "127.0.0.1", // from the hosts file
		"wild.test.":     "127.0.0.1", // wildcard apex
		"a.wild.test.":   "127.0.0.1", // wildcard one level
		"a.b.wild.test.": "127.0.0.1", // wildcard deep (dnsmasq semantics)
		"alias.test.":    "10.1.2.3",  // DNS-alias redirect entry
		"x.alias.test.":  "",          // address=/alias.test/ does NOT cover subdomains of an exact pin? dnsmasq: it does
		"unknown.test.":  "",          // forwarded upstream; the fake upstream answers nothing
		"sibling.exact.": "",          // not a suffix of anything
		"notexact.test.": "",          // suffix-adjacent, must not match
	} {
		resp := queryA(t, addr, name)
		var got string
		if len(resp.Answer) > 0 {
			if a, ok := resp.Answer[0].(*miekg.A); ok {
				got = a.A.String()
			}
		}
		if name == "x.alias.test." {
			// address=/alias.test/ in dnsmasq matches the name AND its
			// subdomains, so the alias pin covers them too.
			if got != "10.1.2.3" {
				t.Errorf("%s = %q, want 10.1.2.3 (wildcard semantics)", name, got)
			}
			continue
		}
		if got != wantIP {
			t.Errorf("%s = %q, want %q", name, got, wantIP)
		}
	}
}

// startStubUpstream answers every A query with ip after delay, on an
// ephemeral loopback UDP port. The returned string is in "ip#port" form,
// ready for a server= line. An empty ip answers with an empty NOERROR —
// the shape of an upstream that knows nothing about the name.
func startStubUpstream(t *testing.T, ip string, delay time.Duration) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &miekg.Server{PacketConn: pc, Handler: miekg.HandlerFunc(func(w miekg.ResponseWriter, r *miekg.Msg) {
		time.Sleep(delay)
		resp := new(miekg.Msg)
		resp.SetReply(r)
		if ip != "" {
			resp.Answer = append(resp.Answer, &miekg.A{
				Hdr: miekg.RR_Header{Name: r.Question[0].Name, Rrtype: miekg.TypeA, Class: miekg.ClassINET, Ttl: 10},
				A:   net.ParseIP(ip),
			})
		}
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

func TestForwardUpstreamFirstAnswerWins(t *testing.T) {
	slow := startStubUpstream(t, "10.0.0.1", 1500*time.Millisecond)
	fast := startStubUpstream(t, "10.0.0.2", 0)
	_, addr := startTestServer(t, "server="+slow+"\nserver="+fast+"\n", "")

	start := time.Now()
	resp := queryA(t, addr, "unknown.test.")
	elapsed := time.Since(start)
	if len(resp.Answer) != 1 {
		t.Fatalf("answers = %d, want the fast upstream's single record", len(resp.Answer))
	}
	if a := resp.Answer[0].(*miekg.A); a.A.String() != "10.0.0.2" {
		t.Errorf("answer = %s, want 10.0.0.2 from the fast upstream", a.A.String())
	}
	if elapsed >= 1500*time.Millisecond {
		t.Errorf("answer took %v; upstreams should be queried concurrently, not in order", elapsed)
	}
}

func TestServeOverTCP(t *testing.T) {
	s, _ := startTestServer(t, "", "127.0.0.1 tcp.test\n")
	addr := s.tcp.Listener.Addr().String()

	m := new(miekg.Msg)
	m.SetQuestion(miekg.Fqdn("tcp.test."), miekg.TypeA)
	client := &miekg.Client{Timeout: 2 * time.Second, Net: "tcp"}
	resp, _, err := client.Exchange(m, addr)
	if err != nil {
		t.Fatalf("tcp query: %v", err)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("answers = %d, want 1", len(resp.Answer))
	}
	if a := resp.Answer[0].(*miekg.A); a.A.String() != "127.0.0.1" {
		t.Errorf("tcp.test. = %s, want 127.0.0.1", a.A.String())
	}
}

func TestReloadPicksUpFileChanges(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "dnsmasq.conf")
	hostsPath := filepath.Join(dir, "dnsmasq.hosts")
	// The pre-registration query forwards upstream; a local empty-answer stub
	// keeps that off the real network (Google's defaults under load timed out
	// the query and failed the test).
	upstream := startStubUpstream(t, "", 0)
	if err := os.WriteFile(confPath, []byte("server="+upstream+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostsPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := New("127.0.0.1", 0, confPath, hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	go func() { _ = s.Serve() }()
	addr := s.Addr()
	waitReady(t, addr)

	if resp := queryA(t, addr, "late.test."); len(resp.Answer) != 0 {
		t.Fatalf("late.test. answered before registration: %v", resp.Answer)
	}

	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 late.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := s.Reload(); err != nil {
			t.Fatal(err)
		}
		// Direct Reload is deterministic; the fsnotify path is exercised by
		// Watch's own test.
		if resp := queryA(t, addr, "late.test."); len(resp.Answer) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("late.test. never resolved after reload")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWatchReloadsOnChange(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "dnsmasq.conf")
	hostsPath := filepath.Join(dir, "dnsmasq.hosts")
	for _, p := range []string{confPath, hostsPath} {
		if err := os.WriteFile(p, []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	s, err := New("127.0.0.1", 0, confPath, hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	go func() { _ = s.Serve() }()
	addr := s.Addr()
	waitReady(t, addr)

	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		_ = s.Watch()
	}()
	t.Cleanup(func() {
		s.Shutdown()
		<-watchDone
	})

	// Atomic-rename write, exactly what fsutil.AtomicWriteFile does.
	tmp := hostsPath + ".tmp"
	if err := os.WriteFile(tmp, []byte("127.0.0.1 watched.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, hostsPath); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp := queryA(t, addr, "watched.test.")
		if len(resp.Answer) == 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("watcher never picked up the rewritten hosts file")
}

func TestAAAAForLocalNameIsEmptyNotNXDOMAIN(t *testing.T) {
	_, addr := startTestServer(t, "", "127.0.0.1 local4.test\n")

	m := new(miekg.Msg)
	m.SetQuestion(miekg.Fqdn("local4.test."), miekg.TypeAAAA)
	client := &miekg.Client{Timeout: 2 * time.Second, Net: "udp"}
	resp, _, err := client.Exchange(m, addr)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Rcode != miekg.RcodeSuccess || len(resp.Answer) != 0 {
		t.Errorf("AAAA = rcode %d, %d answers; want NOERROR with none", resp.Rcode, len(resp.Answer))
	}
}

func TestForwardingServfailsWithoutUpstream(t *testing.T) {
	// Upstream points at a port nothing listens on: the query must come back
	// SERVFAIL rather than hang or return a forged answer.
	_, addr := startTestServer(t, "server=127.0.0.1#1\n", "")

	m := new(miekg.Msg)
	m.SetQuestion(miekg.Fqdn("external.example."), miekg.TypeA)
	client := &miekg.Client{Timeout: 5 * time.Second, Net: "udp"}
	resp, _, err := client.Exchange(m, addr)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if resp.Rcode != miekg.RcodeServerFailure {
		t.Errorf("rcode = %d, want SERVFAIL", resp.Rcode)
	}
}

func TestParseConfAndHostsEdgeCases(t *testing.T) {
	z, err := loadZones(filepath.Join(t.TempDir(), "missing.conf"), filepath.Join(t.TempDir(), "missing.hosts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.upstream) != 2 || z.upstream[0].host != constants.GoogleDNS1 {
		t.Errorf("missing files: upstream = %v, want the Google defaults", z.upstream)
	}

	z, err = loadZones("/nonexistent/x", "/nonexistent/y")
	if err != nil {
		t.Fatal(err)
	}
	_ = z

	conf := "address=/bad newline/127.0.0.1\n" + // no second slash → skipped
		"address=/ok.test/127.0.0.1\n" +
		"server=notanip\n" + // skipped
		"server=9.9.9.9#5353\n" +
		"hostsdir=/etc/whatever\n" + // ignored
		"no-resolv\n" // ignored
	hosts := "127.0.0.1 a.test b.test\n" + // second name also registered
		"10.0.0.1 foreign.test\n" + // non-loopback → not ours
		"garbage-line\n"
	z, err = loadZones(writeTemp(t, "c.conf", conf), writeTemp(t, "h.hosts", hosts))
	if err != nil {
		t.Fatal(err)
	}
	if !wildcardCovers(z, "ok.test.") {
		t.Error("ok.test missing (address= entries are wildcards)")
	}
	if _, ok := z.exact["a.test."]; !ok {
		t.Error("a.test missing")
	}
	if _, ok := z.exact["b.test."]; !ok {
		t.Error("b.test (alias column) missing")
	}
	if _, ok := z.exact["foreign.test."]; ok {
		t.Error("non-loopback host entry must not become a local record")
	}
	var have9999, have5353 bool
	for _, up := range z.upstream {
		if up.host == "9.9.9.9" && up.port == 5353 {
			have5353 = true
		}
		if up.host == constants.GoogleDNS1 {
			have9999 = true
		}
	}
	if have9999 || !have5353 {
		t.Errorf("upstream = %v; want only the valid server= entry", z.upstream)
	}
}

// wildcardCovers mirrors lookupA's suffix logic for zone-inspection tests.
func wildcardCovers(z *ZoneSnapshot, name string) bool {
	for _, w := range z.wildcards {
		if name == strings.TrimPrefix(w.suffix, ".") || strings.HasSuffix(name, w.suffix) {
			return true
		}
	}
	return false
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCheckNameAnsweredWithoutZones(t *testing.T) {
	_, addr := startTestServer(t, "", "")
	if !PingAddr(addr) {
		t.Fatal("health check failed on an empty-zones server")
	}
	if !strings.Contains(CheckName, "srv-dns-check") {
		t.Errorf("CheckName = %q", CheckName)
	}
}

// TestSetZonesFeedsStructuredConfig exercises the primary layer: entries
// pushed via SetZones answer immediately, the file layer keeps answering
// beneath them (hand-written entries), and a primary exact entry shadows a
// same-named file entry.
func TestSetZonesFeedsStructuredConfig(t *testing.T) {
	upstream := startStubUpstream(t, "10.9.9.9", 0)
	s, addr := startTestServer(t, "server="+upstream+"\n", "127.0.0.1 hand.test\n127.0.0.1 shadow.test\n")

	z := NewZoneSnapshot()
	z.PinExact("reg.test", "127.0.0.1")
	z.PinWildcard("wild.test", "127.0.0.1")
	z.PinExact("shadow.test", "10.0.0.1")
	z.PinExact("bad.test", "not-an-ip")
	s.SetZones(z)

	for name, wantIP := range map[string]string{
		"reg.test.":      "127.0.0.1", // primary exact
		"wild.test.":     "127.0.0.1", // primary wildcard apex
		"a.b.wild.test.": "127.0.0.1", // primary wildcard, deep (address=/name/ semantics)
		"shadow.test.":   "10.0.0.1",  // primary wins over the file entry
		"hand.test.":     "127.0.0.1", // file-only entry stays visible
		"bad.test.":      "10.9.9.9",  // invalid pin dropped, query forwarded to the stub
	} {
		resp := queryA(t, addr, name)
		got := answerIP(resp)
		if got != wantIP {
			t.Errorf("%s = %q, want %q", name, got, wantIP)
		}
	}
}

// answerIP returns the first A record's IP, or "" when the answer is empty.
func answerIP(resp *miekg.Msg) string {
	for _, rr := range resp.Answer {
		if a, ok := rr.(*miekg.A); ok {
			return a.A.String()
		}
	}
	return ""
}

// TestSetZonesUpstreamPrecedence pins the forwarder rules: an explicit
// config.yml list (the primary layer's upstreams) overrides the files, an
// empty one defers to the files' server= lines.
func TestSetZonesUpstreamPrecedence(t *testing.T) {
	fileUp := startStubUpstream(t, "10.0.0.1", 0)
	s, addr := startTestServer(t, "server="+fileUp+"\n", "")

	s.SetZones(NewZoneSnapshot())
	if got := answerIP(queryA(t, addr, "unknown.test.")); got != "10.0.0.1" {
		t.Errorf("primary without upstreams: forwarded to %q, want the file's 10.0.0.1", got)
	}

	cfgUp := startStubUpstream(t, "10.0.0.2", 0)
	z := NewZoneSnapshot()
	z.SetUpstream(cfgUp)
	s.SetZones(z)
	if got := answerIP(queryA(t, addr, "unknown.test.")); got != "10.0.0.2" {
		t.Errorf("primary with upstreams: forwarded to %q, want the primary's 10.0.0.2", got)
	}
}

// TestSetZonesRemovalFallsThroughToFiles: a name the structured config no
// longer lists stops being served locally (the file layer is expected to
// re-render without it; here the stub upstream answers instead, proving the
// query left the local zones).
func TestSetZonesRemovalFallsThroughToFiles(t *testing.T) {
	upstream := startStubUpstream(t, "10.0.0.1", 0)
	s, addr := startTestServer(t, "server="+upstream+"\n", "127.0.0.1 file.test\n")

	z := NewZoneSnapshot()
	z.PinExact("both.test", "127.0.0.1")
	s.SetZones(z)
	if got := answerIP(queryA(t, addr, "both.test.")); got != "127.0.0.1" {
		t.Fatalf("both.test = %q, want the primary's 127.0.0.1", got)
	}

	s.SetZones(NewZoneSnapshot())
	if got := answerIP(queryA(t, addr, "both.test.")); got != "10.0.0.1" {
		t.Errorf("removed both.test = %q, want the forwarded %q (no longer a local zone)", got, "10.0.0.1")
	}
	if got := answerIP(queryA(t, addr, "file.test.")); got != "127.0.0.1" {
		t.Errorf("file.test = %q, want the fallback's 127.0.0.1", got)
	}
}

// TestReloadKeepsPrimaryLayer: a file reload must not drop the structured
// snapshot installed by SetZones.
func TestReloadKeepsPrimaryLayer(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "dnsmasq.conf")
	hostsPath := filepath.Join(dir, "dnsmasq.hosts")
	for _, p := range []string{confPath, hostsPath} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New("127.0.0.1", 0, confPath, hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	go func() { _ = s.Serve() }()
	addr := s.Addr()
	waitReady(t, addr)

	z := NewZoneSnapshot()
	z.PinExact("reg.test", "127.0.0.1")
	s.SetZones(z)
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 late.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"reg.test.", "late.test."} {
		if got := answerIP(queryA(t, addr, name)); got != "127.0.0.1" {
			t.Errorf("%s = %q after reload, want 127.0.0.1 (primary and fallback must coexist)", name, got)
		}
	}
}
