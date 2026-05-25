package valet

import "testing"

func TestIsActiveOutput(t *testing.T) {
	cases := map[string]bool{
		"active\n":    true,
		"active":      true,
		"active  \n":  true,
		"inactive\n":  false,
		"failed\n":    false,
		"activating": false,
		"":            false,
		"unknown":     false,
	}
	for in, want := range cases {
		if got := isActiveOutput([]byte(in)); got != want {
			t.Errorf("isActiveOutput(%q) = %v, want %v", in, got, want)
		}
	}
}
