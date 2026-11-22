// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"bytes"
	"io"
	"os"
	"runtime"
	"testing"

	"github.com/armon/circbuf"
)

// copyTarget returns the writer a pooledCopyWriter wraps, or nil for any
// other writer, so a test can check which buffer an exec.Cmd stream fills.
func copyTarget(w io.Writer) io.Writer {
	if p, ok := w.(pooledCopyWriter); ok {
		return p.w
	}
	return nil
}

// copyFromPipe copies data through a pipe into dst the way os/exec copies a
// process's output: io.Copy with the read end of an *os.File pipe as source.
func copyFromPipe(t *testing.T, dst io.Writer, data []byte) int64 {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	go func() {
		_, _ = w.Write(data)
		_ = w.Close()
	}()
	n, err := io.Copy(dst, r)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPooledCopyWriterCopiesEverything(t *testing.T) {
	t.Parallel()
	// Larger than the copy buffer, so the copy takes several reads.
	data := bytes.Repeat([]byte("0123456789abcdef"), 3*outputCopyBufferSize/16+5)

	buf, err := circbuf.NewBuffer(int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	n := copyFromPipe(t, newPooledCopyWriter(buf), data)

	if n != int64(len(data)) {
		t.Fatalf("copied %d bytes, want %d", n, len(data))
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Fatal("copied bytes differ from the input")
	}
}

func TestPooledCopyWriterKeepsTheBufferLimit(t *testing.T) {
	t.Parallel()
	buf, err := circbuf.NewBuffer(8)
	if err != nil {
		t.Fatal(err)
	}
	copyFromPipe(t, newPooledCopyWriter(buf), []byte("0123456789"))

	if got := buf.String(); got != "23456789" {
		t.Fatalf("buffer holds %q, want the last 8 bytes %q", got, "23456789")
	}
}

// TestPooledCopyWriterReusesTheCopyBuffer guards the reason the wrapper
// exists: without ReadFrom, io.Copy allocates outputCopyBufferSize bytes for
// every copy.
func TestPooledCopyWriterReusesTheCopyBuffer(t *testing.T) {
	// Not parallel: the measurement reads process-wide allocation counters.
	buf, err := circbuf.NewBuffer(1024)
	if err != nil {
		t.Fatal(err)
	}
	dst := newPooledCopyWriter(buf)
	data := []byte("hello\n")
	copyFromPipe(t, dst, data) // fill the pool

	const runs = 20
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		copyFromPipe(t, dst, data)
	}
	runtime.ReadMemStats(&after)

	// A garbage collection empties the pool, so one refill may happen; a
	// fresh buffer per copy would put the average at outputCopyBufferSize.
	perCopy := (after.TotalAlloc - before.TotalAlloc) / runs
	if perCopy >= outputCopyBufferSize/4 {
		t.Fatalf("each copy allocated %d bytes on average, want well below %d", perCopy, outputCopyBufferSize)
	}
}
