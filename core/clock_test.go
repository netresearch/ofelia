package core

import (
	"testing"
	"time"
)

func TestRealClock_Now(t *testing.T) {
	t.Parallel()

	clock := NewRealClock()
	before := time.Now()
	now := clock.Now()
	after := time.Now()

	if now.Before(before) || now.After(after) {
		t.Error("RealClock.Now() returned unexpected time")
	}
}

func TestFakeClock_Now(t *testing.T) {
	t.Parallel()

	start := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	if !clock.Now().Equal(start) {
		t.Errorf("Expected %v, got %v", start, clock.Now())
	}
}

func TestFakeClock_Advance(t *testing.T) {
	t.Parallel()

	start := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	clock.Advance(1 * time.Hour)

	expected := start.Add(1 * time.Hour)
	if !clock.Now().Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, clock.Now())
	}
}

func TestFakeClock_Ticker(t *testing.T) {
	t.Parallel()

	start := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	ticker := clock.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	clock.Advance(100 * time.Millisecond)
	select {
	case <-ticker.C():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("First tick did not fire")
	}

	clock.Advance(100 * time.Millisecond)
	select {
	case <-ticker.C():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Second tick did not fire")
	}

	clock.Advance(100 * time.Millisecond)
	select {
	case <-ticker.C():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Third tick did not fire")
	}
}

func TestFakeClock_After(t *testing.T) {
	t.Parallel()

	start := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	fired := make(chan bool, 1)
	ch := clock.After(50 * time.Millisecond)

	go func() {
		<-ch
		fired <- true
	}()

	clock.Advance(25 * time.Millisecond)

	select {
	case <-fired:
		t.Error("After fired too early")
	case <-time.After(10 * time.Millisecond):
	}

	clock.Advance(25 * time.Millisecond)

	select {
	case <-fired:
	case <-time.After(100 * time.Millisecond):
		t.Error("After did not fire after sufficient advance")
	}
}

func TestFakeClock_Sleep(t *testing.T) {
	t.Parallel()

	start := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	done := make(chan struct{})

	go func() {
		clock.Sleep(100 * time.Millisecond)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)

	clock.Advance(100 * time.Millisecond)

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("Sleep did not complete after advance")
	}
}

func TestFakeClock_ZeroDuration(t *testing.T) {
	t.Parallel()

	clock := NewFakeClock(time.Now())

	ch := clock.After(0)
	select {
	case <-ch:
	case <-time.After(10 * time.Millisecond):
		t.Error("After(0) should fire immediately")
	}

	clock.Sleep(0)
}

func TestFakeClock_TickerStop(t *testing.T) {
	t.Parallel()

	clock := NewFakeClock(time.Now())
	ticker := clock.NewTicker(100 * time.Millisecond)

	if clock.TickerCount() != 1 {
		t.Errorf("Expected 1 active ticker, got %d", clock.TickerCount())
	}

	ticker.Stop()

	if clock.TickerCount() != 0 {
		t.Errorf("Expected 0 active tickers after stop, got %d", clock.TickerCount())
	}
}

func TestDefaultClock(t *testing.T) {
	original := GetDefaultClock()
	defer SetDefaultClock(original)

	fakeClock := NewFakeClock(time.Now())
	SetDefaultClock(fakeClock)

	if GetDefaultClock() != fakeClock {
		t.Error("SetDefaultClock did not work")
	}
}
