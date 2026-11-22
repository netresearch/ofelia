// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import "testing"

// BenchmarkLocalJobRun measures one local job run end to end, with its
// output copied into the execution buffers. B/op shows the copy buffers
// os/exec would otherwise allocate for each output stream.
func BenchmarkLocalJobRun(b *testing.B) {
	job := NewLocalJob()
	job.Command = "echo hello"

	for b.Loop() {
		execution, err := NewExecution()
		if err != nil {
			b.Fatal(err)
		}
		if err := job.Run(&Context{Execution: execution}); err != nil {
			b.Fatal(err)
		}
		execution.Cleanup()
	}
}
