package dnsd

import (
	"net"
	"testing"

	miekg "github.com/miekg/dns"

	"github.com/stubbedev/srv/internal/racedetect"
)

// nopResponseWriter satisfies miekg.ResponseWriter. Like the real writers
// it must not keep the message past WriteMsg (replies are pooled), so with
// capture set it packs the reply there; with capture off it allocates
// nothing, so allocation counts measure the handler alone.
type nopResponseWriter struct {
	capture bool
	packed  []byte
}

var loopbackAddr = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53}

func (w *nopResponseWriter) LocalAddr() net.Addr  { return loopbackAddr }
func (w *nopResponseWriter) RemoteAddr() net.Addr { return loopbackAddr }
func (w *nopResponseWriter) WriteMsg(m *miekg.Msg) error {
	if w.capture {
		var err error
		w.packed, err = m.Pack()
		return err
	}
	return nil
}
func (w *nopResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (w *nopResponseWriter) Close() error              { return nil }
func (w *nopResponseWriter) TsigStatus() error         { return nil }
func (w *nopResponseWriter) TsigTimersOnly(bool)       {}
func (w *nopResponseWriter) Hijack()                   {}

func localQueryServer(tb testing.TB) *Server {
	tb.Helper()
	s := &Server{}
	z := newZoneSnapshot()
	z.PinExact("exact.test", "127.0.0.1")
	z.PinWildcard("wild.test", "127.0.0.1")
	s.zones.Store(z)
	return s
}

func question(name string, qtype uint16) *miekg.Msg {
	m := new(miekg.Msg)
	m.SetQuestion(name, qtype)
	return m
}

// TestLocalAnswersAllocateNothing guards the query path: answering a local
// name — exact, wildcard, or the empty answer for a non-A type — allocates
// nothing in this package (miekg's packing is the writer's cost). Each reply
// must still pack to the right answer.
func TestLocalAnswersAllocateNothing(t *testing.T) {
	s := localQueryServer(t)
	w := &nopResponseWriter{}
	for _, tc := range []struct {
		q       *miekg.Msg
		answers int
	}{
		{question("exact.test.", miekg.TypeA), 1},
		{question("a.b.wild.test.", miekg.TypeA), 1},
		{question("a.wild.test.", miekg.TypeAAAA), 0},
		{question(CheckName, miekg.TypeA), 1},
	} {
		w.capture = true
		s.handleQuery(w, tc.q)
		w.capture = false
		var got miekg.Msg
		if err := got.Unpack(w.packed); err != nil {
			t.Fatal(err)
		}
		if got.Id != tc.q.Id || !got.Response || len(got.Question) != 1 || len(got.Answer) != tc.answers {
			t.Errorf("%s: reply id=%d response=%v q=%d answers=%d", tc.q.Question[0].Name, got.Id, got.Response, len(got.Question), len(got.Answer))
		}
		if tc.answers == 1 {
			if a, ok := got.Answer[0].(*miekg.A); !ok || !a.A.Equal(loopbackA) {
				t.Errorf("%s: answer %v, want 127.0.0.1", tc.q.Question[0].Name, got.Answer[0])
			}
		}

		if racedetect.Enabled {
			continue // allocation counts are meaningless under -race
		}
		if n := testing.AllocsPerRun(500, func() { s.handleQuery(w, tc.q) }); n != 0 {
			t.Errorf("%s type %d: %v allocs per query, want 0", tc.q.Question[0].Name, tc.q.Question[0].Qtype, n)
		}
	}
}

func BenchmarkHandleQueryLocalA(b *testing.B) {
	s := localQueryServer(b)
	w := &nopResponseWriter{}
	q := question("a.b.wild.test.", miekg.TypeA)
	b.ReportAllocs()
	for b.Loop() {
		s.handleQuery(w, q)
	}
}
