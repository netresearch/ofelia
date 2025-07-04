package core

import (
	"reflect"
	"testing"
)

func TestComposeJobBuildCommandRun(t *testing.T) {
	job := &ComposeJob{File: "compose.yml", Service: "svc"}
	job.Command = `echo "foo bar"`
	exec, err := NewExecution()
	if err != nil {
		t.Fatalf("NewExecution error: %v", err)
	}
	ctx := &Context{Execution: exec}
	cmd, err := job.buildCommand(ctx)
	if err != nil {
		t.Fatalf("buildCommand error: %v", err)
	}
	wantArgs := []string{"docker", "compose", "-f", "compose.yml", "run", "--rm", "svc", "echo", "foo bar"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Errorf("unexpected args: %v, want %v", cmd.Args, wantArgs)
	}
	if cmd.Stdout != exec.OutputStream {
		t.Errorf("expected stdout to be execution output buffer")
	}
	if cmd.Stderr != exec.ErrorStream {
		t.Errorf("expected stderr to be execution error buffer")
	}
}

func TestComposeJobBuildCommandExec(t *testing.T) {
	job := &ComposeJob{File: "compose.yml", Service: "svc", Exec: true}
	job.Command = `echo "foo bar"`
	exec, err := NewExecution()
	if err != nil {
		t.Fatalf("NewExecution error: %v", err)
	}
	ctx := &Context{Execution: exec}
	cmd, err := job.buildCommand(ctx)
	if err != nil {
		t.Fatalf("buildCommand error: %v", err)
	}
	wantArgs := []string{"docker", "compose", "-f", "compose.yml", "exec", "svc", "echo", "foo bar"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Errorf("unexpected args: %v, want %v", cmd.Args, wantArgs)
	}
	if cmd.Stdout != exec.OutputStream {
		t.Errorf("expected stdout to be execution output buffer")
	}
	if cmd.Stderr != exec.ErrorStream {
		t.Errorf("expected stderr to be execution error buffer")
	}
}
