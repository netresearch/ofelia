package core

import (
	"reflect"
	"testing"
)

func TestComposeJobBuildCommand(t *testing.T) {
	tests := []struct {
		name     string
		job      *ComposeJob
		wantArgs []string
	}{
		{
			name: "Run command",
			job: &ComposeJob{
				BareJob: BareJob{Command: `echo "foo bar"`},
				File:    "compose.yml",
				Service: "svc",
			},
			wantArgs: []string{"docker", "compose", "-f", "compose.yml", "run", "--rm", "svc", "echo", "foo bar"},
		},
		{
			name: "Exec command",
			job: &ComposeJob{
				BareJob: BareJob{Command: `echo "foo bar"`},
				File:    "compose.yml",
				Service: "svc",
				Exec:    true,
			},
			wantArgs: []string{"docker", "compose", "-f", "compose.yml", "exec", "svc", "echo", "foo bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec, err := NewExecution()
			if err != nil {
				t.Fatalf("NewExecution error: %v", err)
			}
			ctx := &Context{Execution: exec}
			cmd := tt.job.buildCommand(ctx)
			if !reflect.DeepEqual(cmd.Args, tt.wantArgs) {
				t.Errorf("unexpected args: %v, want %v", cmd.Args, tt.wantArgs)
			}
			if cmd.Stdout != exec.OutputStream {
				t.Errorf("expected stdout to be execution output buffer")
			}
			if cmd.Stderr != exec.ErrorStream {
				t.Errorf("expected stderr to be execution error buffer")
			}
		})
	}
}

// Removed as part of refactoring into table-driven test.
