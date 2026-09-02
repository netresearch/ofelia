// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"testing"
	"time"
)

// TestParseMaxRuntime covers the single parser shared by the API
// (web.newRunJobFromRequest) and the state-file reload path
// (cli.buildPersistedRunJob) so their validation cannot drift apart the
// way RunJob.Delete once did across those same two call sites.
func TestParseMaxRuntime(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{name: "empty means no override", input: "", want: 0},
		{name: "valid duration", input: "30m", want: 30 * time.Minute},
		{name: "valid duration with hours", input: "2h", want: 2 * time.Hour},
		{name: "zero duration is explicitly allowed", input: "0s", want: 0},
		{name: "unparseable string", input: "not-a-duration", wantErr: true},
		{name: "bare number without unit", input: "30", wantErr: true},
		{name: "negative duration rejected", input: "-5m", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseMaxRuntime(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseMaxRuntime(%q) = %v, nil; want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMaxRuntime(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("ParseMaxRuntime(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
