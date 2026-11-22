// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"io"
	"sync"
)

// outputCopyBufferSize matches the buffer io.Copy allocates on its own.
const outputCopyBufferSize = 32 * 1024

// outputCopyBuffers holds the buffers that copy a job process's output into
// its execution buffers.
var outputCopyBuffers = sync.Pool{
	New: func() any {
		buf := make([]byte, outputCopyBufferSize)
		return &buf
	},
}

// pooledCopyWriter is the writer given to exec.Cmd as Stdout or Stderr.
//
// A writer that is not an *os.File makes os/exec copy the process's pipe
// into it with io.Copy. That copy allocates a new 32 KiB buffer for every
// stream of every run, unless the writer has a ReadFrom method. ReadFrom here
// copies with a buffer from outputCopyBuffers instead.
type pooledCopyWriter struct {
	w io.Writer
}

var _ io.ReaderFrom = pooledCopyWriter{}

// newPooledCopyWriter wraps w for use as an exec.Cmd output stream.
func newPooledCopyWriter(w io.Writer) pooledCopyWriter {
	return pooledCopyWriter{w: w}
}

func (p pooledCopyWriter) Write(b []byte) (int, error) {
	return p.w.Write(b) //nolint:wrapcheck // a plain pass-through to the wrapped writer
}

// ReadFrom copies src into the wrapped writer with a pooled buffer.
func (p pooledCopyWriter) ReadFrom(src io.Reader) (int64, error) {
	bufp, _ := outputCopyBuffers.Get().(*[]byte)
	defer outputCopyBuffers.Put(bufp)
	// Hiding the wrapped writer's methods keeps CopyBuffer from handing the
	// copy to a ReadFrom of its own, which would skip the pooled buffer. src
	// arrives from os/exec without a WriteTo, so CopyBuffer reads with buf.
	return io.CopyBuffer(struct{ io.Writer }{p.w}, src, *bufp) //nolint:wrapcheck // errors belong to the caller's copy
}
