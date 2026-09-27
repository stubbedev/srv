package logfile

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"time"
)

// maxLine is the longest line the readers accept; longer lines (a huge
// stack trace) are an error rather than a silent truncation.
const maxLine = 1 << 20

// followPoll is how often Follow checks for new lines and for a rotation.
const followPoll = 200 * time.Millisecond

// Tail returns the last n lines accepted by match (nil accepts every line),
// oldest first, reading the live file and then older generations only as far
// back as it needs. n <= 0 returns every matching line in the log. A missing
// live file is an error wrapping fs.ErrNotExist.
func Tail(path string, n int, match func(string) bool) ([]string, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	var chunks [][]string // newest generation first
	total := 0
	for g := 0; g <= Generations && (n <= 0 || total < n); g++ {
		lines, err := tailFile(generation(path, g), n-total, match)
		if errors.Is(err, os.ErrNotExist) {
			break // generations are contiguous: nothing older exists
		}
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, lines)
		total += len(lines)
	}
	slices.Reverse(chunks)
	out := slices.Concat(chunks...)
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	if out == nil {
		out = []string{} // an empty log is no lines, not "no answer"
	}
	return out, nil
}

// tailFile returns the last n matching lines of one file (all when n <= 0),
// holding at most n lines in memory.
func tailFile(path string, n int, match func(string) bool) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var ring []string
	if n > 0 {
		ring = make([]string, 0, n)
	}
	next := 0 // once the ring is full, the slot of the oldest line
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLine)
	for scanner.Scan() {
		line := scanner.Text()
		if match != nil && !match(line) {
			continue
		}
		if n <= 0 || len(ring) < n {
			ring = append(ring, line)
			continue
		}
		ring[next] = line
		next = (next + 1) % n
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return append(ring[next:], ring[:next]...), nil
}

// Follow calls emit with each complete line appended to the log from now on,
// until ctx is done. It follows the log across rotations (the path naming a
// new file) and truncations, and never emits a partial line.
func Follow(ctx context.Context, path string, emit func(string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	var pending []byte // an unterminated tail, held until its newline lands
	buf := make([]byte, 64<<10)
	drain := func() error {
		for {
			n, err := f.Read(buf)
			offset += int64(n)
			pending = append(pending, buf[:n]...)
			for {
				i := bytes.IndexByte(pending, '\n')
				if i < 0 {
					break
				}
				emit(string(pending[:i]))
				pending = pending[i+1:]
			}
			if len(pending) > maxLine {
				pending = pending[:0] // runaway line: drop rather than grow forever
			}
			if errors.Is(err, io.EOF) || n == 0 {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}

	t := time.NewTicker(followPoll)
	defer t.Stop()
	for {
		if err := drain(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		cur, err := f.Stat()
		if err != nil {
			return err
		}
		switch st, err := os.Stat(path); {
		case err != nil:
			// Mid-rotation: the live file is momentarily absent.
		case !os.SameFile(cur, st):
			// Rotated: finish the old file, then start the new one at 0.
			if err := drain(); err != nil {
				return err
			}
			next, err := os.Open(path)
			if err != nil {
				continue
			}
			_ = f.Close()
			f, offset, pending = next, 0, pending[:0]
		case st.Size() < offset:
			// Truncated in place: restart from the top.
			if offset, err = f.Seek(0, io.SeekStart); err != nil {
				return err
			}
			pending = pending[:0]
		}
	}
}
