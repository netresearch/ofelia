package core

import (
	"reflect"
	"testing"
	"time"

	. "gopkg.in/check.v1"
)

type hashJob struct {
	Str string `hash:"true"`
	Num int    `hash:"true"`
	Flg bool   `hash:"true"`
}

func TestGetHash_SupportedKinds(t *testing.T) {
	var h string
	val := &hashJob{Str: "x", Num: 7, Flg: true}
	if err := GetHash(reflect.TypeOf(val).Elem(), reflect.ValueOf(val).Elem(), &h); err != nil {
		t.Fatalf("GetHash error: %v", err)
	}
	if h == "" {
		t.Fatalf("expected non-empty hash")
	}
}

func TestExecutionStopFlagsAndDuration(t *testing.T) {
	e := &Execution{}
	start := time.Now()
	e.Date = start
	e.Start()
	e.Stop(ErrSkippedExecution)
	if !e.Skipped || e.Failed {
		t.Fatalf("expected skipped true, failed false: %+v", e)
	}
	if e.Duration <= 0 {
		t.Fatalf("expected positive duration, got %v", e.Duration)
	}
	// Failure path
	e = &Execution{}
	e.Start()
	e.Stop(assertError{})
	if e.Error == nil || !e.Failed {
		t.Fatalf("expected failure with error: %+v", e)
	}
}

type assertError struct{}

func (assertError) Error() string { return "boom" }

func TestBareJobHistory(t *testing.T) {
	j := &BareJob{HistoryLimit: 2}
	// Add three executions, ensure we keep last 2
	for i := 0; i < 3; i++ {
		e := &Execution{}
		j.SetLastRun(e)
	}
	if j.GetLastRun() == nil {
		t.Fatalf("expected last run to be set")
	}
	if len(j.GetHistory()) != 2 {
		t.Fatalf("expected history length 2, got %d", len(j.GetHistory()))
	}
}

func TestExecutionGetStdout(t *testing.T) {
	e, err := NewExecution()
	if err != nil {
		t.Fatalf("NewExecution error: %v", err)
	}

	// Test with live buffer
	testOutput := "test stdout content"
	_, err = e.OutputStream.Write([]byte(testOutput))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	if got := e.GetStdout(); got != testOutput {
		t.Errorf("GetStdout() with live buffer = %q, want %q", got, testOutput)
	}

	// Test after cleanup (captured content)
	e.Cleanup()
	if got := e.GetStdout(); got != testOutput {
		t.Errorf("GetStdout() after cleanup = %q, want %q", got, testOutput)
	}

	// Test with nil buffer and captured content
	if e.OutputStream != nil {
		t.Error("OutputStream should be nil after cleanup")
	}
	if e.CapturedStdout != testOutput {
		t.Errorf("CapturedStdout = %q, want %q", e.CapturedStdout, testOutput)
	}
}

func TestExecutionGetStderr(t *testing.T) {
	e, err := NewExecution()
	if err != nil {
		t.Fatalf("NewExecution error: %v", err)
	}

	// Test with live buffer
	testError := "test stderr content"
	_, err = e.ErrorStream.Write([]byte(testError))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	if got := e.GetStderr(); got != testError {
		t.Errorf("GetStderr() with live buffer = %q, want %q", got, testError)
	}

	// Test after cleanup (captured content)
	e.Cleanup()
	if got := e.GetStderr(); got != testError {
		t.Errorf("GetStderr() after cleanup = %q, want %q", got, testError)
	}

	// Test with nil buffer and captured content
	if e.ErrorStream != nil {
		t.Error("ErrorStream should be nil after cleanup")
	}
	if e.CapturedStderr != testError {
		t.Errorf("CapturedStderr = %q, want %q", e.CapturedStderr, testError)
	}
}

func TestExecutionOutputCleanup(t *testing.T) {
	e, err := NewExecution()
	if err != nil {
		t.Fatalf("NewExecution error: %v", err)
	}

	// Write test content
	stdoutContent := "stdout test"
	stderrContent := "stderr test"

	_, err = e.OutputStream.Write([]byte(stdoutContent))
	if err != nil {
		t.Fatalf("Write stdout error: %v", err)
	}

	_, err = e.ErrorStream.Write([]byte(stderrContent))
	if err != nil {
		t.Fatalf("Write stderr error: %v", err)
	}

	// Verify buffers are active before cleanup
	if e.OutputStream == nil || e.ErrorStream == nil {
		t.Fatal("Buffers should be active before cleanup")
	}

	// Cleanup and verify capture
	e.Cleanup()

	// Verify buffers are nil after cleanup
	if e.OutputStream != nil || e.ErrorStream != nil {
		t.Error("Buffers should be nil after cleanup")
	}

	// Verify content is captured
	if e.CapturedStdout != stdoutContent {
		t.Errorf("CapturedStdout = %q, want %q", e.CapturedStdout, stdoutContent)
	}
	if e.CapturedStderr != stderrContent {
		t.Errorf("CapturedStderr = %q, want %q", e.CapturedStderr, stderrContent)
	}

	// Verify GetStdout/GetStderr work after cleanup
	if got := e.GetStdout(); got != stdoutContent {
		t.Errorf("GetStdout() after cleanup = %q, want %q", got, stdoutContent)
	}
	if got := e.GetStderr(); got != stderrContent {
		t.Errorf("GetStderr() after cleanup = %q, want %q", got, stderrContent)
	}
}

func TestExecutionEmptyOutput(t *testing.T) {
	e, err := NewExecution()
	if err != nil {
		t.Fatalf("NewExecution error: %v", err)
	}

	// Test empty buffers
	if got := e.GetStdout(); got != "" {
		t.Errorf("GetStdout() with empty buffer = %q, want empty string", got)
	}
	if got := e.GetStderr(); got != "" {
		t.Errorf("GetStderr() with empty buffer = %q, want empty string", got)
	}

	// Test after cleanup with empty buffers
	e.Cleanup()
	if got := e.GetStdout(); got != "" {
		t.Errorf("GetStdout() after cleanup with empty buffer = %q, want empty string", got)
	}
	if got := e.GetStderr(); got != "" {
		t.Errorf("GetStderr() after cleanup with empty buffer = %q, want empty string", got)
	}
}

// Register a trivial gocheck hook to keep suite loader intact for other tests
func Test(t *testing.T) { TestingT(t) }
