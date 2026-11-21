// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"errors"
	"testing"
)

// A server without a job builder refuses to create a job instead of
// building one its own way.
func TestJobFromRequest_RefusedWithoutBuilder(t *testing.T) {
	t.Parallel()
	_, err := (&Server{}).jobFromRequest(&jobRequest{Name: "n", Type: "local", Command: "echo"})
	if !errors.Is(err, errNoJobBuilder) {
		t.Fatalf("err = %v, want errNoJobBuilder", err)
	}
}
