package core

import (
	"context"
	"log/slog"
	"testing"
)

// stubJob implements Job with minimal methods for testing.
type stubJob struct {
	name string
}

func (j *stubJob) GetName() string           { return j.name }
func (j *stubJob) GetSchedule() string       { return "" }
func (j *stubJob) GetCommand() string        { return "" }
func (j *stubJob) ShouldRunOnStartup() bool  { return false }
func (j *stubJob) Middlewares() []Middleware { return nil }
func (j *stubJob) Use(...Middleware)         {}
func (j *stubJob) Run(*Context) error        { return nil }
func (j *stubJob) Hash() (string, error)     { return "stub-hash", nil }
func (j *stubJob) Running() int32            { return 0 }
func (j *stubJob) NotifyStart()              {}
func (j *stubJob) NotifyStop()               {}
func (j *stubJob) GetCronJobID() uint64      { return 0 }
func (j *stubJob) SetCronJobID(id uint64)    {}
func (j *stubJob) GetHistory() []*Execution  { return nil }

// capturingHandler is a slog.Handler that captures log records for testing.
type capturingHandler struct {
	records []slog.Record
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func (h *capturingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(_ string) slog.Handler      { return h }

// TestContextLogDefault verifies that Context.Log uses Info level when no error or skip.
func TestContextLogDefault(t *testing.T) {
	handler := &capturingHandler{}
	logger := slog.New(handler)
	job := &stubJob{name: "jobName"}
	exec := &Execution{ID: "ID"}
	ctx := &Context{Logger: logger, Job: job, Execution: exec}
	ctx.Log("hello")
	if len(handler.records) != 1 {
		t.Fatalf("expected 1 log call, got %d", len(handler.records))
	}
	record := handler.records[0]
	if record.Level != slog.LevelInfo {
		t.Errorf("expected level Info, got %s", record.Level)
	}
}

// TestContextLogError verifies that Context.Log uses Error level when execution failed.
func TestContextLogError(t *testing.T) {
	handler := &capturingHandler{}
	logger := slog.New(handler)
	job := &stubJob{name: "jobName"}
	exec := &Execution{ID: "ID", Failed: true}
	ctx := &Context{Logger: logger, Job: job, Execution: exec}
	ctx.Log("oops")
	if len(handler.records) != 1 {
		t.Fatalf("expected 1 log call, got %d", len(handler.records))
	}
	record := handler.records[0]
	if record.Level != slog.LevelError {
		t.Errorf("expected level Error, got %s", record.Level)
	}
}

// TestContextLogSkipped verifies that Context.Log uses Warn level when execution skipped.
func TestContextLogSkipped(t *testing.T) {
	handler := &capturingHandler{}
	logger := slog.New(handler)
	job := &stubJob{name: "jobName"}
	exec := &Execution{ID: "ID", Skipped: true}
	ctx := &Context{Logger: logger, Job: job, Execution: exec}
	ctx.Log("skip")
	if len(handler.records) != 1 {
		t.Fatalf("expected 1 log call, got %d", len(handler.records))
	}
	record := handler.records[0]
	if record.Level != slog.LevelWarn {
		t.Errorf("expected level Warn, got %s", record.Level)
	}
}

// TestContextWarn verifies that Context.Warn always uses Warn level.
func TestContextWarn(t *testing.T) {
	handler := &capturingHandler{}
	logger := slog.New(handler)
	job := &stubJob{name: "jobName"}
	exec := &Execution{ID: "ID"}
	ctx := &Context{Logger: logger, Job: job, Execution: exec}
	ctx.Warn("caution")
	if len(handler.records) != 1 {
		t.Fatalf("expected 1 log call, got %d", len(handler.records))
	}
	record := handler.records[0]
	if record.Level != slog.LevelWarn {
		t.Errorf("expected level Warn, got %s", record.Level)
	}
}
