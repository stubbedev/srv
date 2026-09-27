package dnsd

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	miekg "github.com/miekg/dns"
)

// startCountingUpstream is startStubUpstream plus a query counter.
func startCountingUpstream(t *testing.T, ip string) (string, *atomic.Int64) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var count atomic.Int64
	srv := &miekg.Server{PacketConn: pc, Handler: miekg.HandlerFunc(func(w miekg.ResponseWriter, r *miekg.Msg) {
		count.Add(1)
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
	_, port, _ := net.SplitHostPort(pc.LocalAddr().String())
	return "127.0.0.1#" + port, &count
}

// startSilentUpstream accepts queries and never answers: a resolver that is
// up but wedged, the worst case for a forwarder.
func startSilentUpstream(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	_, port, _ := net.SplitHostPort(pc.LocalAddr().String())
	return "127.0.0.1#" + port
}

// A healthy first upstream answers alone: the forwarder must not multiply
// every lookup by the number of configured resolvers.
func TestForwardUpstreamHealthyFirstIsOnlyQuery(t *testing.T) {
	first, firstCount := startCountingUpstream(t, "10.0.0.1")
	second, secondCount := startCountingUpstream(t, "10.0.0.2")
	_, addr := startTestServer(t, "server="+first+"\nserver="+second+"\n", "")

	const queries = 20
	for i := range queries {
		if ip := answerIP(queryA(t, addr, fmt.Sprintf("h%d.test.", i))); ip != "10.0.0.1" {
			t.Fatalf("answer = %q, want the first upstream's 10.0.0.1", ip)
		}
	}
	if got := firstCount.Load(); got != queries {
		t.Errorf("first upstream saw %d queries, want %d", got, queries)
	}
	if got := secondCount.Load(); got != 0 {
		t.Errorf("second upstream saw %d queries, want 0 while the first answers promptly", got)
	}
}

// A wedged first upstream costs one hedge delay, not a full upstream timeout.
func TestForwardUpstreamSilentFirstHedgesQuickly(t *testing.T) {
	silent := startSilentUpstream(t)
	backup, _ := startCountingUpstream(t, "10.0.0.2")
	_, addr := startTestServer(t, "server="+silent+"\nserver="+backup+"\n", "")

	start := time.Now()
	if ip := answerIP(queryA(t, addr, "hedge.test.")); ip != "10.0.0.2" {
		t.Fatalf("answer = %q, want the backup's 10.0.0.2", ip)
	}
	if elapsed := time.Since(start); elapsed >= upstreamTimeout/2 {
		t.Errorf("answer took %v; a silent upstream should only delay by ~%v", elapsed, hedgeDelay)
	}
}

// Wildcard lookups walk the query's labels and the most specific entry wins,
// whatever order the entries were declared in.
func TestLookupAMostSpecificWildcardWins(t *testing.T) {
	z := newZoneSnapshot()
	z.PinWildcard("test", "10.0.0.1")
	z.PinWildcard("app.test", "10.0.0.2")
	z.PinWildcard("api.app.test", "10.0.0.3")
	for name, want := range map[string]string{
		"test.":            "10.0.0.1",
		"other.test.":      "10.0.0.1",
		"app.test.":        "10.0.0.2",
		"x.y.app.test.":    "10.0.0.2",
		"api.app.test.":    "10.0.0.3",
		"v1.api.app.test.": "10.0.0.3",
		"napp.test.":       "10.0.0.1", // a label boundary, not a string suffix
	} {
		if ip, _ := lookupA(z, name); ip.String() != want {
			t.Errorf("%s = %q, want %q", name, ip, want)
		}
	}
	if _, ok := lookupA(z, "example.com."); ok {
		t.Error("example.com. matched a .test wildcard")
	}
}
