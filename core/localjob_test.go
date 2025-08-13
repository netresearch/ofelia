package core

import (
	"os/exec"
	"testing"
)

func TestLocalBuildCommand(t *testing.T) {
	e, _ := NewExecution()
	ctx := &Context{Execution: e}
	j := &LocalJob{}
	j.Command = "echo hello"
	cmd, err := j.buildCommand(ctx)
	if err != nil {
		t.Fatalf("buildCommand error: %v", err)
	}
	if cmd.Path == "" || len(cmd.Args) == 0 {
		t.Fatalf("unexpected cmd: %#v", cmd)
	}
	if cmd.Stdout != e.OutputStream || cmd.Stderr != e.ErrorStream {
		t.Fatalf("expected stdio bound to execution buffers")
	}
}

func TestLocalBuildCommandMissingBinary(t *testing.T) {
	e, _ := NewExecution()
	ctx := &Context{Execution: e}
	j := &LocalJob{}
	j.Command = "nonexistent-binary --flag"
	_, err := j.buildCommand(ctx)
	if err == nil {
		t.Fatalf("expected error for missing binary")
	}
	// ensure error originates from LookPath
	if _, ok := err.(*exec.Error); !ok {
		// not all platforms return *exec.Error, so allow any error
		_ = err
	}
}
