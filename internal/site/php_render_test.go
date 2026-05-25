package site

import (
	"sort"
	"testing"
)

func TestKnownPHPExtensionsSorted(t *testing.T) {
	exts := KnownPHPExtensions()
	if len(exts) == 0 {
		t.Fatal("empty extension list")
	}
	if !sort.StringsAreSorted(exts) {
		t.Error("KnownPHPExtensions not sorted")
	}
	exts[0] = "MUTATED"
	again := KnownPHPExtensions()
	if again[0] == "MUTATED" {
		t.Error("KnownPHPExtensions returned reference to package slice")
	}
}

func TestIsBuiltinPHPExtension(t *testing.T) {
	if !IsBuiltinPHPExtension("json") {
		t.Error("json should be builtin")
	}
	if IsBuiltinPHPExtension("redis") {
		t.Error("redis should NOT be builtin")
	}
}

func TestNonBuiltinExtensions(t *testing.T) {
	in := []string{"json", "redis", "hash", "imagick"}
	out := NonBuiltinExtensions(in)
	if len(out) != 2 {
		t.Errorf("got %v, want 2 entries", out)
	}
}
