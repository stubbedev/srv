package domain

import (
	"fmt"
	"testing"
	"time"
)

func TestMatchSuffix(t *testing.T) {
	reg := map[string]string{"test": "tld", "app.test": "app", "fq.test.": "fqdn"}
	for name, want := range map[string]string{
		"test":          "tld",
		"x.test":        "tld",
		"app.test":      "app",
		"a.b.app.test":  "app",
		"napp.test":     "tld", // label boundary, not string suffix
		"fq.test.":      "fqdn",
		"x.fq.test.":    "fqdn",
		"example.com":   "",
		"":              "",
		".":             "",
		"trailing.dot.": "",
	} {
		got, ok := MatchSuffix(reg, name)
		if got != want || ok != (want != "") {
			t.Errorf("MatchSuffix(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
}

func TestMatchParent(t *testing.T) {
	reg := map[string]int{"app.test": 1}
	if _, ok := MatchParent(reg, "app.test"); ok {
		t.Error("MatchParent matched the name itself")
	}
	if v, ok := MatchParent(reg, "x.y.app.test"); !ok || v != 1 {
		t.Errorf("MatchParent(x.y.app.test) = %v, %v", v, ok)
	}
	if _, ok := MatchParent(reg, "test"); ok {
		t.Error("single label matched")
	}
}

// TestMatchSuffixScalesWithNameNotRegistrations guards against a linear scan
// creeping back in: ten thousand registrations must cost about what one does.
func TestMatchSuffixScalesWithNameNotRegistrations(t *testing.T) {
	build := func(n int) map[string]bool {
		m := make(map[string]bool, n)
		for i := range n {
			m[fmt.Sprintf("site%d.test.", i)] = true
		}
		return m
	}
	// Best of several fixed-size runs: robust to scheduler noise without
	// the second-long ramp of testing.Benchmark.
	perOp := func(m map[string]bool) time.Duration {
		const iters = 20000
		best := time.Duration(1<<63 - 1)
		for range 5 {
			start := time.Now()
			for range iters {
				MatchSuffix(m, "deep.miss.example.com.")
			}
			best = min(best, time.Since(start)/iters)
		}
		return best
	}
	small, large := perOp(build(1)), perOp(build(10000))
	// A linear scan over 10k entries costs ~1000× one probe; allow generous
	// slack for map-size cache effects.
	if large > 10*small+100*time.Nanosecond {
		t.Errorf("%v with 10k registrations vs %v with 1 — cost grows with registrations", large, small)
	}
}

func BenchmarkMatchSuffix10k(b *testing.B) {
	m := make(map[string]bool, 10000)
	for i := range 10000 {
		m[fmt.Sprintf("site%d.test.", i)] = true
	}
	for b.Loop() {
		MatchSuffix(m, "deep.sub.site9999.test.")
	}
}
