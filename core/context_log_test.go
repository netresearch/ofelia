package core

import (
	"reflect"
	"testing"
)

// logCall records logger method calls.
type logCall struct {
	method string
	format string
	args   []interface{}
}

// stubLogger implements Logger and records calls.
type stubLogger struct {
	calls []logCall
}

func (l *stubLogger) Criticalf(format string, args ...interface{}) {
	l.calls = append(l.calls, logCall{"Criticalf", format, args})
}

func (l *stubLogger) Debugf(format string, args ...interface{}) {
	l.calls = append(l.calls, logCall{"Debugf", format, args})
}

func (l *stubLogger) Errorf(format string, args ...interface{}) {
	l.calls = append(l.calls, logCall{"Errorf", format, args})
}

func (l *stubLogger) Noticef(format string, args ...interface{}) {
	l.calls = append(l.calls, logCall{"Noticef", format, args})
}

func (l *stubLogger) Warningf(format string, args ...interface{}) {
	l.calls = append(l.calls, logCall{"Warningf", format, args})
}

// stubJob implements Job with minimal methods.
type stubJob struct {
	name string
}

func (j *stubJob) GetName() string           { return j.name }
func (j *stubJob) GetSchedule() string       { return "" }
func (j *stubJob) GetCommand() string        { return "" }
func (j *stubJob) Middlewares() []Middleware { return nil }
func (j *stubJob) Use(...Middleware)         {}
func (j *stubJob) Run(*Context) error        { return nil }
func (j *stubJob) Running() int32            { return 0 }
func (j *stubJob) NotifyStart()              {}
func (j *stubJob) NotifyStop()               {}
func (j *stubJob) GetCronJobID() int         { return 0 }
func (j *stubJob) SetCronJobID(id int)       {}
func (j *stubJob) GetHistory() []*Execution  { return nil }

// TestContextLogDefault verifies that Context.Log uses Noticef when no error or skip.
func TestContextLogDefault(t *testing.T) {
	logger := &stubLogger{}
	job := &stubJob{name: "jobName"}
	exec := &Execution{ID: "ID"}
	ctx := &Context{Logger: logger, Job: job, Execution: exec}
	ctx.Log("hello")
	if len(logger.calls) != 1 {
		t.Fatalf("expected 1 log call, got %d", len(logger.calls))
	}
	call := logger.calls[0]
	if call.method != "Noticef" {
		t.Errorf("expected method Noticef, got %s", call.method)
	}
	if call.format != logPrefix {
		t.Errorf("expected format %q, got %q", logPrefix, call.format)
	}
	wantArgs := []interface{}{job.name, exec.ID, "hello"}
	if !reflect.DeepEqual(call.args, wantArgs) {
		t.Errorf("expected args %v, got %v", wantArgs, call.args)
	}
}

// TestContextLogError verifies that Context.Log uses Errorf when execution failed.
func TestContextLogError(t *testing.T) {
	logger := &stubLogger{}
	job := &stubJob{name: "jobName"}
	exec := &Execution{ID: "ID", Failed: true}
	ctx := &Context{Logger: logger, Job: job, Execution: exec}
	ctx.Log("oops")
	if len(logger.calls) != 1 {
		t.Fatalf("expected 1 log call, got %d", len(logger.calls))
	}
	call := logger.calls[0]
	if call.method != "Errorf" {
		t.Errorf("expected method Errorf, got %s", call.method)
	}
}

// TestContextLogSkipped verifies that Context.Log uses Warningf when execution skipped.
func TestContextLogSkipped(t *testing.T) {
	logger := &stubLogger{}
	job := &stubJob{name: "jobName"}
	exec := &Execution{ID: "ID", Skipped: true}
	ctx := &Context{Logger: logger, Job: job, Execution: exec}
	ctx.Log("skip")
	if len(logger.calls) != 1 {
		t.Fatalf("expected 1 log call, got %d", len(logger.calls))
	}
	call := logger.calls[0]
	if call.method != "Warningf" {
		t.Errorf("expected method Warningf, got %s", call.method)
	}
}

// TestContextWarn verifies that Context.Warn always uses Warningf.
func TestContextWarn(t *testing.T) {
	logger := &stubLogger{}
	job := &stubJob{name: "jobName"}
	exec := &Execution{ID: "ID"}
	ctx := &Context{Logger: logger, Job: job, Execution: exec}
	ctx.Warn("caution")
	if len(logger.calls) != 1 {
		t.Fatalf("expected 1 log call, got %d", len(logger.calls))
	}
	call := logger.calls[0]
	if call.method != "Warningf" {
		t.Errorf("expected method Warningf, got %s", call.method)
	}
	wantArgs := []interface{}{job.name, exec.ID, "caution"}
	if !reflect.DeepEqual(call.args, wantArgs) {
		t.Errorf("expected args %v, got %v", wantArgs, call.args)
	}
}
