// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"testing"
	"time"

	"github.com/netresearch/ofelia/test/testutil"
)

// TestGetHealthDoesNotReadMemStatsPerCall pins that the health report is
// served from the snapshot the periodic system check takes.
//
// GetHealth used to call runtime.ReadMemStats on every request, which
// stops the world. /ready is exempt from rate limiting (an orchestrator
// probe answered 429 reads as unhealthy), so an unauthenticated caller
// could force one GC pause per request with no budget at all — exactly
// the cost the exemption comment cited when it declined to exempt
// /health.
//
// The probe: allocate between two reports. A per-call ReadMemStats sees
// the new allocation and reports a different Alloc; a snapshot does not.
func TestGetHealthDoesNotReadMemStatsPerCall(t *testing.T) {
	hc := NewHealthChecker(nil, nil, "test")
	defer hc.Stop()

	// Let the initial background pass finish first: it takes its own
	// snapshot, and the next one is a full checkInterval away, so
	// nothing else can refresh the numbers during the probe below.
	testutil.Eventually(t, func() bool {
		_, ok := hc.GetHealth().Checks["system"]
		return ok
	}, testutil.WithTimeout(2*time.Second), testutil.WithMessage("system check did not run"))

	before := hc.GetHealth().System.MemoryAlloc

	// Big enough that Alloc cannot land on the same value by chance, and
	// kept alive past the second report so the allocation is not
	// collected before it would be observed.
	ballast := make([]byte, 64<<20)
	for i := 0; i < len(ballast); i += 4096 {
		ballast[i] = 1
	}

	after := hc.GetHealth().System.MemoryAlloc
	if before != after {
		t.Fatalf("MemoryAlloc changed between reports (%d -> %d): GetHealth is reading MemStats per call", before, after)
	}

	if ballast[0] != 1 {
		t.Fatal("ballast optimized away")
	}
}
