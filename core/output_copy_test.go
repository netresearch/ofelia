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

func TestPooledCopyWriterWritePassesThrough(t *testing.T) {
	t.Parallel()
	buf, err := circbuf.NewBuffer(64)
	if err != nil {
		t.Fatal(err)
	}
	n, err := newPooledCopyWriter(buf).Write([]byte("direct"))
	if err != nil || n != len("direct") {
		t.Fatalf("Write = %d, %v; want %d, nil", n, err, len("direct"))
	}
	if got := buf.String(); got != "direct" {
		t.Fatalf("buffer holds %q, want %q", got, "direct")
	}
}

// TestPooledCopyWriterReusesTheCopyBuffer guards the reason the wrapper
// exists: without ReadFrom, io.Copy allocates outputCopyBufferSize bytes for
// every copy.
//
// The allocation counter is process-wide, so goroutines of other tests add
// noise, and under -race sync.Pool drops a quarter of the buffers it is
// given back. The test therefore counts single copies that allocate less
// than half a buffer: without the pool none does, with it most do. Control
// copies into the bare buffer, interleaved, must show the allocation.
func TestPooledCopyWriterReusesTheCopyBuffer(t *testing.T) {
	// Not parallel: the measurement reads process-wide allocation counters.
	buf, err := circbuf.NewBuffer(1024)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := newPooledCopyWriter(buf)
	data := []byte("hello\n")
	copyFromPipe(t, wrapped, data) // fill the pool

	allocated := func(dst io.Writer) uint64 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		copyFromPipe(t, dst, data)
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}

	const runs = 21
	const small = outputCopyBufferSize / 2
	pooledSmall, bareSmall := 0, 0
	for range runs {
		if allocated(wrapped) < small {
			pooledSmall++
		}
		if allocated(buf) < small {
			bareSmall++
		}
	}

	if bareSmall > runs/3 {
		t.Fatalf("%d of %d control copies allocated less than %d bytes: the measurement cannot see the copy buffer",
			bareSmall, runs, small)
	}
	if pooledSmall < runs/3 {
		t.Fatalf("only %d of %d copies through the wrapper allocated less than %d bytes; want at least %d",
			pooledSmall, runs, small, runs/3)
	}
}
