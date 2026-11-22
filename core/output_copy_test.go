// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"bytes"
	"io"
	"os"
	"runtime"
	"slices"
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
//
// The allocation counter is process-wide, so other tests' goroutines add
// noise. The test therefore compares medians of single copies, interleaved
// with a control copy into the bare buffer, which must show the allocation.
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
	pooled := make([]uint64, 0, runs)
	bare := make([]uint64, 0, runs)
	for range runs {
		pooled = append(pooled, allocated(wrapped))
		bare = append(bare, allocated(buf))
	}
	slices.Sort(pooled)
	slices.Sort(bare)
	pooledMedian, bareMedian := pooled[runs/2], bare[runs/2]

	if bareMedian < outputCopyBufferSize {
		t.Fatalf("control copy allocated %d bytes (median), want at least %d: the measurement cannot see the copy buffer",
			bareMedian, outputCopyBufferSize)
	}
	if pooledMedian >= outputCopyBufferSize/2 {
		t.Fatalf("copy through the wrapper allocated %d bytes (median), want well below %d (control: %d)",
			pooledMedian, outputCopyBufferSize, bareMedian)
	}
}
