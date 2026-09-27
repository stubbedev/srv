package pool

import "testing"

func TestPoolReturnsUsableValues(t *testing.T) {
	var p Pool[[4]int]
	v := p.Get()
	if v == nil || *v != [4]int{} {
		t.Fatalf("Get on empty pool = %v, want a zero value", v)
	}
	v[0] = 7
	p.Put(v)
	if got := p.Get(); got == nil {
		t.Fatal("Get returned nil")
	}
}
