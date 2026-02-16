package core

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// cronCaptureHandler records slog records for CronUtils tests.
type cronCaptureHandler struct {
	records []slog.Record
}

func (h *cronCaptureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *cronCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func (h *cronCaptureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *cronCaptureHandler) WithGroup(_ string) slog.Handler      { return h }

func TestCronUtilsInfoForwardsArgs(t *testing.T) {
	handler := &cronCaptureHandler{}
	logger := slog.New(handler)
	cu := NewCronUtils(logger)
	cu.Info("msg", "a", 1, "b", 2)
	if len(handler.records) != 1 {
		t.Fatalf("expected 1 call, got %d", len(handler.records))
	}
	record := handler.records[0]
	if record.Level != slog.LevelDebug {
		t.Errorf("expected level Debug, got %s", record.Level)
	}
	if record.Message != "msg" {
		t.Errorf("expected message %q, got %q", "msg", record.Message)
	}
}

func TestCronUtilsErrorForwardsArgs(t *testing.T) {
	handler := &cronCaptureHandler{}
	logger := slog.New(handler)
	cu := NewCronUtils(logger)
	err := errors.New("boom")
	cu.Error(err, "fail", "k", "v")
	if len(handler.records) != 1 {
		t.Fatalf("expected 1 call, got %d", len(handler.records))
	}
	record := handler.records[0]
	if record.Level != slog.LevelError {
		t.Errorf("expected level Error, got %s", record.Level)
	}
	if record.Message != "fail" {
		t.Errorf("expected message %q, got %q", "fail", record.Message)
	}
}
