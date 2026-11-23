// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what
// fn wrote. The pipe is drained while fn runs: a Windows pipe buffers about
// 4 KB, so reading only after fn returns blocks a larger write forever.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	os.Stdout = oldStdout
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}
