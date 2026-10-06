// Package logfile is the daemon log: a size-capped, rotating, buffered
// writer and the readers that understand its rotation. Writer and readers
// share one naming scheme (path, path.1 … path.Generations, newest first),
// so a reader can never lose lines to a rotation it does not know about.
//
// Lines are the unit of consistency. Every Write must carry whole lines (one
// zerolog event, one formatted message); the writer never splits a Write
// across a flush or across a rotation, so readers only ever see complete
// lines on disk.
package logfile

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	// MaxSize is the size at which the live file rotates.
	MaxSize = 10 << 20
	// Generations is how many rotated files are kept besides the live one.
	Generations = 3
	// FlushInterval bounds how long a buffered line waits to reach disk.
	FlushInterval = time.Second
	// bufferSize is the write buffer: large enough that a request burst is
	// one write syscall, small enough to lose little on a crash.
	bufferSize = 64 << 10
)

// generation returns the path of rotated file n (0 is the live file).
func generation(path string, n int) string {
	if n == 0 {
		return path
	}
	return path + "." + strconv.Itoa(n)
}

// Writer appends lines to a log file through a buffer that flushes every
// FlushInterval, rotating the file once it passes MaxSize. It is safe for
// concurrent use; each Write lands on disk contiguously.
type Writer struct {
	mu   sync.Mutex
	path string
	perm os.FileMode
	// maxSize is MaxSize; in-package tests lower it to exercise rotation.
	maxSize int64
	f       *os.File
	buf     *bufio.Writer
	size    int64
	// closed latches Close: a nil f alone no longer means closed, because a
	// failed rotation also nils it and must stay retryable.
	closed bool

	stop chan struct{}
	done chan struct{}
}

// Open opens (or creates) path for appending and starts the flush loop.
func Open(path string, perm os.FileMode) (*Writer, error) {
	w := &Writer{path: path, perm: perm, maxSize: MaxSize, stop: make(chan struct{}), done: make(chan struct{})}
	if err := w.open(); err != nil {
		return nil, err
	}
	go w.flushLoop()
	return w, nil
}

// Write buffers p, which must hold whole lines. A p that does not fit the
// buffer's free space flushes the buffer first, so p is never split.
// A rotation whose reopen failed leaves f nil but the writer open: the next
// Write retries the open instead of failing with ErrClosed forever.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if w.f == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if len(p) > w.buf.Available() && w.buf.Buffered() > 0 {
		if err := w.buf.Flush(); err != nil {
			return 0, err
		}
	}
	n, err := w.buf.Write(p) // larger than the buffer: bufio writes it through whole
	w.size += int64(n)
	if err == nil && w.size >= w.maxSize {
		err = w.rotate()
	}
	return n, err
}

// Flush writes buffered lines to the file now. The daemon's own messages
// flush immediately; access events ride the interval.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.f == nil {
		return nil
	}
	return w.buf.Flush()
}

// Close flushes and closes the file. Later Writes fail with os.ErrClosed.
// The flush loop is always stopped, even when a failed rotation left no open
// file — the early return used to leak the goroutine and its ticker.
func (w *Writer) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	var err error
	if w.f != nil {
		err = w.buf.Flush()
		if cerr := w.f.Close(); err == nil {
			err = cerr
		}
		w.f = nil
	}
	w.mu.Unlock()
	close(w.stop)
	<-w.done
	return err
}

// open (re)opens the live file for appending. Callers hold mu or own w.
func (w *Writer) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, w.perm)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.f, w.size = f, st.Size()
	if w.buf == nil {
		w.buf = bufio.NewWriterSize(f, bufferSize)
	} else {
		w.buf.Reset(f)
	}
	return nil
}

// rotate shifts path.N-1 → path.N … path → path.1 and reopens path. The
// oldest generation is overwritten by the rename. Callers hold mu.
func (w *Writer) rotate() error {
	if err := w.buf.Flush(); err != nil {
		return err
	}
	if err := w.f.Close(); err != nil {
		return err
	}
	w.f = nil
	var renameErr error
	for n := Generations; n > 0; n-- {
		if err := os.Rename(generation(w.path, n-1), generation(w.path, n)); err != nil && !os.IsNotExist(err) && renameErr == nil {
			renameErr = fmt.Errorf("rotate %s: %w", w.path, err)
		}
	}
	// Reopen even when a rename failed: a stuck rotation must not stop the
	// log; the live file just keeps growing until the next attempt.
	if err := w.open(); err != nil {
		return err
	}
	if renameErr != nil {
		w.size = 0 // retry after another MaxSize, not on every write
	}
	return renameErr
}

func (w *Writer) flushLoop() {
	defer close(w.done)
	t := time.NewTicker(FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-t.C:
			_ = w.Flush()
		}
	}
}
