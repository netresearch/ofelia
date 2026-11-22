// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"testing"
)

func TestExecJobValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, container, command string
		want                     error
	}{
		{"valid", "app", "id", nil},
		{"no container", "", "id", ErrExecContainerRequired},
		{"no command", "app", "", ErrEmptyCommand},
		{"blank command", "app", "  ", ErrEmptyCommand},
	}
	for _, tc := range cases {
		j := &ExecJob{}
		j.Container, j.Command = tc.container, tc.command
		if err := j.Validate(); !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
			t.Errorf("%s: Validate() = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestLocalJobValidate(t *testing.T) {
	t.Parallel()
	j := &LocalJob{}
	j.Command = " "
	if err := j.Validate(); !errors.Is(err, ErrEmptyCommand) {
		t.Errorf("blank command: Validate() = %v, want %v", err, ErrEmptyCommand)
	}
	j.Command = "true"
	if err := j.Validate(); err != nil {
		t.Errorf("valid: Validate() = %v", err)
	}
}

func TestComposeJobValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, file, service string
		wantErr             bool
	}{
		{"valid", "compose.yml", "db", false},
		{"bad service", "compose.yml", "bad name", true},
		{"file in /etc", "/etc/compose.yml", "db", true},
	}
	for _, tc := range cases {
		j := &ComposeJob{}
		j.File, j.Service = tc.file, tc.service
		if err := j.Validate(); (err != nil) != tc.wantErr {
			t.Errorf("%s: Validate() = %v, want error %v", tc.name, err, tc.wantErr)
		}
	}
}
