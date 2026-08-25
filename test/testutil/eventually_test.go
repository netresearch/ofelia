// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package testutil

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestEventually_ConditionTrueImmediately(t *testing.T) {
	t.Parallel()

	result := Eventually(t, func() bool {
		return true
	}, WithTimeout(100*time.Millisecond))

	if !result {
		t.Error("Expected Eventually to return true when condition is immediately true")
	}
}

func TestEventually_ConditionBecomesTrueAfterDelay(t *testing.T) {
	t.Parallel()

	var counter atomic.Int32
	result := Eventually(t, func() bool {
		return counter.Add(1) >= 3
	}, WithTimeout(1*time.Second), WithInterval(10*time.Millisecond))

	if !result {
		t.Error("Expected Eventually to return true when condition becomes true")
	}
}

func TestEventually_Timeout(t *testing.T) {
	t.Parallel()

	// Use a mock T to capture the error
	mockT := &mockTB{}

	result := Eventually(mockT, func() bool {
		return false
	}, WithTimeout(50*time.Millisecond), WithInterval(10*time.Millisecond))

	if result {
		t.Error("Expected Eventually to return false on timeout")
	}
	if !mockT.failed {
		t.Error("Expected Eventually to call Errorf on timeout")
	}
}

func TestNever_ConditionStaysFalse(t *testing.T) {
	t.Parallel()

	result := Never(t, func() bool {
		return false
	}, WithTimeout(50*time.Millisecond), WithInterval(10*time.Millisecond))

	if !result {
		t.Error("Expected Never to return true when condition stays false")
	}
}

func TestNever_ConditionBecomesTrue(t *testing.T) {
	t.Parallel()

	mockT := &mockTB{}
	var counter atomic.Int32

	result := Never(mockT, func() bool {
		return counter.Add(1) >= 2
	}, WithTimeout(1*time.Second), WithInterval(10*time.Millisecond))

	if result {
		t.Error("Expected Never to return false when condition becomes true")
	}
	if !mockT.failed {
		t.Error("Expected Never to call Errorf when condition becomes true")
	}
}

func TestWaitForChan_ReceivesValue(t *testing.T) {
	t.Parallel()

	ch := make(chan int, 1)
	ch <- 42

	val, ok := WaitForChan(t, ch, 100*time.Millisecond)
	if !ok {
		t.Error("Expected WaitForChan to succeed")
	}
	if val != 42 {
		t.Errorf("Expected value 42, got %d", val)
	}
}

func TestWaitForChan_ChannelClosed(t *testing.T) {
	t.Parallel()

	ch := make(chan int)
	close(ch)

	_, ok := WaitForChan(t, ch, 100*time.Millisecond)
	if !ok {
		t.Error("Expected WaitForChan to succeed on closed channel")
	}
}

func TestWaitForChan_Timeout(t *testing.T) {
	t.Parallel()

	mockT := &mockTB{}
	ch := make(chan int)

	_, ok := WaitForChan(mockT, ch, 50*time.Millisecond)
	if ok {
		t.Error("Expected WaitForChan to fail on timeout")
	}
	if !mockT.failed {
		t.Error("Expected WaitForChan to call Errorf on timeout")
	}
}

func TestWaitForClose_ChannelCloses(t *testing.T) {
	t.Parallel()

	ch := make(chan struct{})
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(ch)
	}()

	ok := WaitForClose(t, ch, 100*time.Millisecond)
	if !ok {
		t.Error("Expected WaitForClose to succeed")
	}
}

func TestEventuallyWithT_CollectsErrors(t *testing.T) {
	t.Parallel()

	var counter atomic.Int32
	result := EventuallyWithT(t, func(collect *T) bool {
		count := counter.Add(1)
		if count < 3 {
			collect.Errorf("not ready yet, count=%d", count)
			return false
		}
		return true
	}, WithTimeout(1*time.Second), WithInterval(10*time.Millisecond))

	if !result {
		t.Error("Expected EventuallyWithT to return true")
	}
}

func TestT_Failed(t *testing.T) {
	t.Parallel()

	t.Run("no_errors", func(t *testing.T) {
		t.Parallel()
		collect := &T{}
		if collect.Failed() {
			t.Error("Failed() should return false with no errors")
		}
	})

	t.Run("with_errors", func(t *testing.T) {
		t.Parallel()
		collect := &T{}
		collect.Errorf("something went wrong")
		if !collect.Failed() {
			t.Error("Failed() should return true after Errorf")
		}
	})

	t.Run("multiple_errors", func(t *testing.T) {
		t.Parallel()
		collect := &T{}
		collect.Errorf("error 1")
		collect.Errorf("error 2")
		if !collect.Failed() {
			t.Error("Failed() should return true after multiple Errorf calls")
		}
	})
}

func TestWithMessage(t *testing.T) {
	t.Parallel()

	mockT := &mockTB{}

	result := Eventually(mockT, func() bool {
		return false
	}, WithTimeout(50*time.Millisecond), WithInterval(10*time.Millisecond),
		WithMessage("custom timeout message"))

	if result {
		t.Error("Expected Eventually to return false on timeout")
	}
	if !mockT.failed {
		t.Error("Expected Eventually to call Errorf on timeout")
	}
}

func TestEventuallyWithT_Timeout(t *testing.T) {
	t.Parallel()

	mockT := &mockTB{}

	result := EventuallyWithT(mockT, func(collect *T) bool {
		collect.Errorf("still not ready")
		return false
	}, WithTimeout(50*time.Millisecond), WithInterval(10*time.Millisecond))

	if result {
		t.Error("Expected EventuallyWithT to return false on timeout")
	}
	if !mockT.failed {
		t.Error("Expected EventuallyWithT to report failure on timeout")
	}
}

func TestNever_ConditionTrueImmediately(t *testing.T) {
	t.Parallel()

	mockT := &mockTB{}

	result := Never(mockT, func() bool {
		return true
	}, WithTimeout(100*time.Millisecond))

	if result {
		t.Error("Expected Never to return false when condition is immediately true")
	}
	if !mockT.failed {
		t.Error("Expected Never to call Errorf when condition is immediately true")
	}
}

// mockTB is a mock testing.TB for testing timeout behavior.
type mockTB struct {
	testing.TB
	failed bool
}

func (m *mockTB) Helper() {}

func (m *mockTB) Errorf(format string, args ...any) {
	m.failed = true
}
