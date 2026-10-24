// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

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
				Command: `echo "foo bar"`,
				File:    "compose.yml",
				Service: "svc",
			},
			wantArgs: []string{
				"docker", "compose", "-f", "compose.yml", "run", "--rm", "-T",
				"-l", "ofelia.execution=exec-id", "svc", "echo", "foo bar",
			},
		},
		{
			name: "Exec command",
			job: &ComposeJob{
				Command: `echo "foo bar"`,
				File:    "compose.yml",
				Service: "svc",
				Exec:    true,
			},
			wantArgs: []string{"docker", "compose", "-f", "compose.yml", "exec", "svc", "echo", "foo bar"},
		},
		{
			name: "Job from a registered project",
			job: &ComposeJob{
				File:    "compose.yml", // the [job-compose] default; Project.Files replace it
				Service: "backup",
				Project: ComposeProjectContext{
					Files:            []string{"/srv/shop/compose.yaml", "/srv/shop/compose.jobs.yaml"},
					Profiles:         []string{"scheduled-jobs", "extra"},
					EnvFiles:         []string{"/srv/shop/.env.prod"},
					ProjectName:      "shop",
					ProjectDirectory: "/srv/shop",
				},
			},
			wantArgs: []string{
				"docker", "compose",
				"-f", "/srv/shop/compose.yaml", "-f", "/srv/shop/compose.jobs.yaml",
				"--profile", "scheduled-jobs", "--profile", "extra",
				"--env-file", "/srv/shop/.env.prod",
				"-p", "shop", "--project-directory", "/srv/shop",
				"run", "--rm", "-T", "-l", "ofelia.execution=exec-id", "backup",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec, err := NewExecution()
			if err != nil {
				t.Fatalf("NewExecution error: %v", err)
			}
			exec.ID = "exec-id"
			ctx := &Context{Execution: exec}
			cmd, err := tt.job.buildCommand(ctx)
			if err != nil {
				t.Fatalf("buildCommand error: %v", err)
			}
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

func TestComposeJobBuildCommand_RejectsUnsafeProjectValues(t *testing.T) {
	cases := map[string]ComposeProjectContext{
		"file":              {Files: []string{"a.yml; rm -rf /"}},
		"profile":           {Files: []string{"a.yml"}, Profiles: []string{"x$(id)"}},
		"env file":          {Files: []string{"a.yml"}, EnvFiles: []string{"e.env && id"}},
		"project name":      {Files: []string{"a.yml"}, ProjectName: "shop;id"},
		"project directory": {Files: []string{"a.yml"}, ProjectDirectory: "/srv | id"},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			exec, err := NewExecution()
			if err != nil {
				t.Fatalf("NewExecution error: %v", err)
			}
			job := &ComposeJob{Service: "backup", Project: p}
			if _, err := job.buildCommand(&Context{Execution: exec}); err == nil {
				t.Fatalf("expected an error for an unsafe %s", name)
			}
		})
	}
}

func TestComposeJobHash_ChangesWithProjectRevision(t *testing.T) {
	a := &ComposeJob{Service: "backup", Project: ComposeProjectContext{Files: []string{"a.yml"}, Revision: "aaa"}}
	b := &ComposeJob{Service: "backup", Project: ComposeProjectContext{Files: []string{"a.yml"}, Revision: "bbb"}}
	ha, err := a.Hash()
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	hb, err := b.Hash()
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if ha == hb {
		t.Fatalf("a changed project revision must change the job hash, so reload replaces the job")
	}
}
