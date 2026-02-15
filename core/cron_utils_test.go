package core

import (
	"errors"
	"reflect"
	"testing"
)

// stubLogger and logCall are defined in context_log_test.go and reused here.

func TestCronUtilsInfoForwardsArgs(t *testing.T) {
	logger := &stubLogger{}
	cu := NewCronUtils(logger)
	cu.Info("msg", "a", 1, "b", 2)
	if len(logger.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(logger.calls))
	}
	call := logger.calls[0]
	if call.method != "Debugf" {
		t.Errorf("expected method Debugf, got %s", call.method)
	}
	expectedFormat := cronFormatString(4)
	if call.format != expectedFormat {
		t.Errorf("expected format %q, got %q", expectedFormat, call.format)
	}
	wantArgs := []any{"msg", "a", 1, "b", 2}
	if !reflect.DeepEqual(call.args, wantArgs) {
		t.Errorf("expected args %v, got %v", wantArgs, call.args)
	}
}

func TestCronUtilsErrorForwardsArgs(t *testing.T) {
	logger := &stubLogger{}
	cu := NewCronUtils(logger)
	err := errors.New("boom")
	cu.Error(err, "fail", "k", "v")
	if len(logger.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(logger.calls))
	}
	call := logger.calls[0]
	if call.method != "Errorf" {
		t.Errorf("expected method Errorf, got %s", call.method)
	}
	expectedFormat := cronFormatString(4)
	if call.format != expectedFormat {
		t.Errorf("expected format %q, got %q", expectedFormat, call.format)
	}
	wantArgs := []any{"fail", "error", err, "k", "v"}
	if !reflect.DeepEqual(call.args, wantArgs) {
		t.Errorf("expected args %v, got %v", wantArgs, call.args)
	}
}
