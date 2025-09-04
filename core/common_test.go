package core

import (
	"reflect"
	"testing"
	"time"

	. "gopkg.in/check.v1"
)

func TestParseRegistry(t *testing.T) {
	tests := []struct {
		name string
		in   string
		out  string
	}{
		{"no-slash", "busybox", ""},
		{"docker-hub-style", "library/busybox", ""},
		{"registry host", "my.registry:5000/repo/image", "my.registry:5000"},
		{"gcr style", "gcr.io/project/image", "gcr.io"},
		{"three parts", "host/ns/image", "host"},
	}
	for _, tt := range tests {
		if got := parseRegistry(tt.in); got != tt.out {
			t.Fatalf("%s: parseRegistry(%q)=%q want %q", tt.name, tt.in, got, tt.out)
		}
	}
}

// existing TestBuildFindLocalImageOptions present in common_extra_test.go

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

// Register a trivial gocheck hook to keep suite loader intact for other tests
func Test(t *testing.T) { TestingT(t) }
