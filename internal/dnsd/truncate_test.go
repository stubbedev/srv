package dnsd

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	miekg "github.com/miekg/dns"
)

// startFakeUpstream runs a UDP+TCP DNS server pair on one loopback port.
// The handler can branch on w.LocalAddr().Network() to answer UDP and TCP
// differently, which is exactly how a resolver that must truncate behaves.
func startFakeUpstream(t *testing.T, handle miekg.HandlerFunc) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", pc.LocalAddr().String())
	if err != nil {
		_ = pc.Close()
		t.Fatal(err)
	}
	mux := miekg.NewServeMux()
	mux.HandleFunc(".", handle)
	udp := &miekg.Server{PacketConn: pc, Handler: mux}
	tcp := &miekg.Server{Listener: ln, Handler: mux}
	go func() { _ = udp.ActivateAndServe() }()
	go func() { _ = tcp.ActivateAndServe() }()
	t.Cleanup(func() {
		_ = udp.Shutdown()
		_ = tcp.Shutdown()
	})
	return pc.LocalAddr().String()
}

// bigAnswer builds a reply to r carrying n A records — large enough that it
// only fits a TCP exchange or an EDNS0-sized datagram, never plain 512-byte
// UDP.
func bigAnswer(r *miekg.Msg, n int) *miekg.Msg {
	m := new(miekg.Msg)
	m.SetReply(r)
	for i := range n {
		m.Answer = append(m.Answer, &miekg.A{
			Hdr: miekg.RR_Header{Name: r.Question[0].Name, Rrtype: miekg.TypeA, Class: miekg.ClassINET, Ttl: 60},
			A:   net.IPv4(10, 0, byte(i>>8), byte(i)),
		})
	}
	return m
}

// A forwarder answer that arrives truncated must be re-asked over TCP.
// Before the fix, the TC bit passed through unchanged and the client's own
// TCP retry was forwarded upstream over UDP again — large answers could
// never be resolved through srv.
func TestForwarderRetriesTruncatedAnswersOverTCP(t *testing.T) {
	const records = 60
	upstreamAddr := startFakeUpstream(t, func(w miekg.ResponseWriter, r *miekg.Msg) {
		if w.LocalAddr().Network() == "udp" {
			m := bigAnswer(r, 3)
			m.Truncated = true
			_ = w.WriteMsg(m)
			return
		}
		_ = w.WriteMsg(bigAnswer(r, records))
	})
	_, addr := startTestServer(t, "server="+strings.Replace(upstreamAddr, ":", "#", 1)+"\n", "")

	m := new(miekg.Msg)
	m.SetQuestion(miekg.Fqdn("many.example."), miekg.TypeA)
	m.SetEdns0(4096, false)
	client := &miekg.Client{Net: "udp", Timeout: 3 * time.Second}
	resp, _, err := client.Exchange(m, addr)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if resp.Truncated {
		t.Error("client answer still truncated after the TCP retry")
	}
	if len(resp.Answer) != records {
		t.Errorf("answers = %d, want the full %d-record answer over TCP", len(resp.Answer), records)
	}
}

// The answer's owner name must echo the question's case (RFC 1035 4.1.9);
// case-mixing resolvers match the reply against the query they sent.
func TestAnswerEchoesQuestionCase(t *testing.T) {
	_, addr := startTestServer(t, "", "127.0.0.1 case.test\n")

	m := new(miekg.Msg)
	m.SetQuestion("CaSe.TeSt.", miekg.TypeA)
	client := &miekg.Client{Net: "udp", Timeout: 2 * time.Second}
	resp, _, err := client.Exchange(m, addr)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("answers = %d, want 1", len(resp.Answer))
	}
	if got := resp.Answer[0].Header().Name; got != "CaSe.TeSt." {
		t.Errorf("answer owner = %q, want the question's case", got)
	}
}

// stubWriter captures what writeReply sends, with a configurable local
// network ("udp" or "tcp").
type stubWriter struct {
	network string
	written []*miekg.Msg
}

func (s *stubWriter) LocalAddr() net.Addr  { return &addrStub{network: s.network} }
func (s *stubWriter) RemoteAddr() net.Addr { return &addrStub{network: s.network} }
func (s *stubWriter) WriteMsg(m *miekg.Msg) error {
	s.written = append(s.written, m)
	return nil
}
func (s *stubWriter) Write([]byte) (int, error) { return 0, nil }
func (s *stubWriter) Close() error              { return nil }
func (s *stubWriter) Hijack()                   {}
func (s *stubWriter) TsigStatus() error         { return nil }
func (s *stubWriter) TsigTimersOnly(bool)       {}

type addrStub struct{ network string }

func (a *addrStub) Network() string { return a.network }
func (a *addrStub) String() string  { return "127.0.0.1:53" }

func TestWriteReplyTruncatesAndEchoesEDNS(t *testing.T) {
	q := new(miekg.Msg)
	q.SetQuestion(miekg.Fqdn("many.example."), miekg.TypeA)

	// Plain UDP client: 512-byte budget, TC set, answers trimmed.
	w := &stubWriter{network: "udp"}
	writeReply(w, q, bigAnswer(q, 60))
	if len(w.written) != 1 {
		t.Fatalf("writes = %d", len(w.written))
	}
	if !w.written[0].Truncated || len(w.written[0].Answer) == 60 {
		t.Errorf("plain UDP: truncated=%v answers=%d, want TC with a trimmed set", w.written[0].Truncated, len(w.written[0].Answer))
	}

	// EDNS client advertising 4096: everything fits, no TC.
	big := new(miekg.Msg)
	big.SetQuestion(miekg.Fqdn("many.example."), miekg.TypeA)
	big.SetEdns0(4096, false)
	w = &stubWriter{network: "udp"}
	writeReply(w, big, bigAnswer(big, 60))
	if w.written[0].Truncated || len(w.written[0].Answer) != 60 {
		t.Errorf("edns udp: truncated=%v answers=%d, want everything", w.written[0].Truncated, len(w.written[0].Answer))
	}
	if o := w.written[0].IsEdns0(); o == nil || o.UDPSize() != serverUDPSize {
		t.Errorf("edns reply carries %v, want srv's own OPT with size %d", o, serverUDPSize)
	}

	// TCP never truncates.
	w = &stubWriter{network: "tcp"}
	writeReply(w, q, bigAnswer(q, 60))
	if w.written[0].Truncated || len(w.written[0].Answer) != 60 {
		t.Errorf("tcp: truncated=%v answers=%d, want everything", w.written[0].Truncated, len(w.written[0].Answer))
	}
}

// SetZones and Reload both rebuild the serving snapshot from a primary and a
// fallback layer. Without serialization, a Reload that raced a SetZones
// published combine(oldPrimary, newFallback) and silently reverted the
// primary layer until the next event.
func TestSetZonesReloadInterleaveKeepsBothLayers(t *testing.T) {
	s, _ := startTestServer(t, "", "127.0.0.1 fallback.test\n")

	primary := NewZoneSnapshot()
	primary.PinExact("primary.test", "127.0.0.1")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 50 {
			s.SetZones(primary)
		}
	}()
	go func() {
		defer wg.Done()
		for range 50 {
			if err := s.Reload(); err != nil {
				t.Errorf("Reload: %v", err)
			}
		}
	}()
	wg.Wait()

	// Both calls finished: the final snapshot must answer from BOTH layers.
	z := s.zones.Load()
	for _, name := range []string{"primary.test.", "fallback.test."} {
		if ip, ok := lookupA(z, name); !ok || ip == nil {
			t.Errorf("final snapshot lost %s (primary/fallback interleave)", name)
		}
	}
}
