package logfile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func openTest(t *testing.T, maxSize int64) (*Writer, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "daemon.log")
	w, err := Open(path, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	w.maxSize = maxSize
	t.Cleanup(func() { _ = w.Close() })
	return w, path
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// Writes are buffered: nothing reaches disk until a flush, so a request burst
// costs one write syscall instead of one per line.
func TestWriterBuffersUntilFlush(t *testing.T) {
	w, path := openTest(t, MaxSize)
	fmt.Fprintln(w, "one")
	if got := readAll(t, path); got != "" {
		t.Errorf("unflushed line already on disk: %q", got)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, path); got != "one\n" {
		t.Errorf("after flush: %q", got)
	}
}

// The flush loop bounds how long a line waits.
func TestWriterFlushesOnInterval(t *testing.T) {
	w, path := openTest(t, MaxSize)
	fmt.Fprintln(w, "later")
	deadline := time.Now().Add(3 * FlushInterval)
	for readAll(t, path) != "later\n" {
		if time.Now().After(deadline) {
			t.Fatal("line never flushed by the interval loop")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The file rotates at the cap, keeps Generations old files, and every
// line survives whole in exactly one generation.
func TestWriterRotatesWithoutSplittingLines(t *testing.T) {
	w, path := openTest(t, 1024)
	const lines = 400
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Go(func() {
			for i := range lines / 4 {
				fmt.Fprintf(w, "g%d-line-%03d %s\n", g, i, strings.Repeat("x", 20))
			}
		})
	}
	wg.Wait()
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(generation(path, Generations+1)); !os.IsNotExist(err) {
		t.Errorf("generation %d exists; only %d should be kept", Generations+1, Generations)
	}
	for g := 0; g <= Generations; g++ {
		data := readAll(t, generation(path, g))
		if data == "" {
			t.Fatalf("generation %d empty", g)
		}
		if g > 0 && int64(len(data)) > 1024+64 {
			t.Errorf("generation %d is %d bytes, cap 1024", g, len(data))
		}
		for line := range strings.SplitSeq(strings.TrimSuffix(data, "\n"), "\n") {
			if !strings.HasPrefix(line, "g") || !strings.HasSuffix(line, strings.Repeat("x", 20)) {
				t.Fatalf("generation %d holds a split line: %q", g, line)
			}
		}
	}
}

// Tail reads back across generations, oldest first, and only as far as it
// needs to.
func TestTailSpansGenerations(t *testing.T) {
	w, path := openTest(t, 64)
	for i := range 40 {
		fmt.Fprintf(w, "line-%02d\n", i)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	got, err := Tail(path, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"line-35", "line-36", "line-37", "line-38", "line-39"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Tail(5) = %v, want %v", got, want)
	}

	all, err := Tail(path, 0, func(l string) bool { return strings.HasSuffix(l, "0") })
	if err != nil {
		t.Fatal(err)
	}
	// Older lines were rotated out; the survivors must stay in order.
	for i := 1; i < len(all); i++ {
		if all[i-1] >= all[i] {
			t.Fatalf("Tail(all) out of order: %v", all)
		}
	}
	if len(all) == 0 || all[len(all)-1] != "line-30" {
		t.Errorf("Tail(all, match) = %v, want it to end at line-30", all)
	}
}

func TestTailMissingFile(t *testing.T) {
	if _, err := Tail(filepath.Join(t.TempDir(), "nope"), 5, nil); !os.IsNotExist(err) {
		t.Errorf("err = %v, want not-exist", err)
	}
}

// Follow keeps delivering across a rotation, never emits a partial line,
// and stops with its context.
func TestFollowAcrossRotation(t *testing.T) {
	w, path := openTest(t, 128)
	fmt.Fprintln(w, "before-follow")
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var got []string
	done := make(chan error, 1)
	go func() {
		done <- Follow(ctx, path, func(l string) {
			mu.Lock()
			got = append(got, l)
			mu.Unlock()
		})
	}()
	time.Sleep(2 * followPoll) // let Follow seek to the end

	const lines = 30 // ~4 rotations at 128 bytes
	for i := range lines {
		fmt.Fprintf(w, "line-%02d-%s\n", i, strings.Repeat("y", 10))
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(followPoll / 4)
	}
	deadline := time.Now().Add(5 * followPoll)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= lines || time.Now().After(deadline) {
			break
		}
		time.Sleep(followPoll)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != lines {
		t.Fatalf("followed %d lines, want %d: %v", len(got), lines, got)
	}
	for i, l := range got {
		if want := fmt.Sprintf("line-%02d-%s", i, strings.Repeat("y", 10)); l != want {
			t.Errorf("line %d = %q, want %q", i, l, want)
		}
	}
}

// A line whose newline has not been written yet is held, not emitted early.
func TestFollowHoldsPartialLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	lines := make(chan string, 4)
	done := make(chan error, 1)
	go func() { done <- Follow(ctx, path, func(l string) { lines <- l }) }()
	time.Sleep(2 * followPoll)

	_, _ = f.WriteString("hal")
	time.Sleep(2 * followPoll)
	select {
	case l := <-lines:
		t.Fatalf("partial line emitted: %q", l)
	default:
	}
	_, _ = f.WriteString("f\n")
	select {
	case l := <-lines:
		if l != "half" {
			t.Errorf("line = %q, want half", l)
		}
	case <-time.After(5 * followPoll):
		t.Fatal("completed line never emitted")
	}
	cancel()
	<-done
}
