// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"testing"
	"time"

	"github.com/netresearch/ofelia/test"
)

// publishProbeJob writes output and records what SetLastRun receives, at the
// moment it receives it.
type publishProbeJob struct {
	BareJob
	published chan publishedRun
}

type publishedRun struct {
	buffersAttached bool
	stdout, stderr  string
}

func (j *publishProbeJob) Run(ctx *Context) error {
	_, _ = ctx.Execution.OutputStream.Write([]byte("out"))
	_, _ = ctx.Execution.ErrorStream.Write([]byte("err"))
	return nil
}

func (j *publishProbeJob) SetLastRun(e *Execution) {
	j.published <- publishedRun{
		buffersAttached: e.OutputStream != nil || e.ErrorStream != nil,
		stdout:          e.GetStdout(),
		stderr:          e.GetStderr(),
	}
	j.BareJob.SetLastRun(e)
}

// A run is published as the job's last run only after its output has left
// the pooled buffers: a reader on another goroutine, like the web API, must
// not hold a buffer that Execution.Cleanup returns to the pool (#898).
func TestJobWrapper_PublishesRunWithoutPooledBuffers(t *testing.T) {
	s := NewScheduler(test.NewTestLogger())
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	j := &publishProbeJob{
		BareJob:   BareJob{Name: "probe", Schedule: TriggeredSchedule, Command: "true"},
		published: make(chan publishedRun, 1),
	}
	if err := s.AddJob(j); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if err := s.RunJob(context.Background(), "probe"); err != nil {
		t.Fatalf("RunJob: %v", err)
	}

	select {
	case got := <-j.published:
		if got.buffersAttached {
			t.Error("the run was published with its pooled buffers still attached")
		}
		if got.stdout != "out" || got.stderr != "err" {
			t.Errorf("published output = %q / %q, want %q / %q", got.stdout, got.stderr, "out", "err")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run was never published")
	}
}
