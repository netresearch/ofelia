package core

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
	After(d time.Duration) <-chan time.Time
	Sleep(d time.Duration)
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type realClock struct{}

func NewRealClock() Clock {
	return &realClock{}
}

func (c *realClock) Now() time.Time {
	return time.Now()
}

func (c *realClock) NewTicker(d time.Duration) Ticker {
	return &realTicker{ticker: time.NewTicker(d)}
}

func (c *realClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

func (c *realClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

type realTicker struct {
	ticker *time.Ticker
}

func (t *realTicker) C() <-chan time.Time {
	return t.ticker.C
}

func (t *realTicker) Stop() {
	t.ticker.Stop()
}

var defaultClock Clock = NewRealClock()

func SetDefaultClock(c Clock) {
	defaultClock = c
}

func GetDefaultClock() Clock {
	return defaultClock
}

type FakeClock struct {
	mu       sync.RWMutex
	now      time.Time
	tickers  []*fakeTicker
	waiters  []waiter
	advanced chan struct{}
}

type waiter struct {
	deadline time.Time
	ch       chan time.Time
}

func NewFakeClock(start time.Time) *FakeClock {
	return &FakeClock{
		now:      start,
		advanced: make(chan struct{}, 100),
	}
}

func (c *FakeClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *FakeClock) NewTicker(d time.Duration) Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()

	ft := &fakeTicker{
		clock:    c,
		duration: d,
		ch:       make(chan time.Time, 1),
		nextTick: c.now.Add(d),
	}
	c.tickers = append(c.tickers, ft)
	return ft
}

func (c *FakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}

	c.waiters = append(c.waiters, waiter{
		deadline: c.now.Add(d),
		ch:       ch,
	})
	return ch
}

func (c *FakeClock) Sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	<-c.After(d)
}

func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	target := c.now.Add(d)
	c.advanceTo(target)
}

func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.advanceTo(t)
}

func (c *FakeClock) advanceTo(target time.Time) {
	for {
		earliest := c.findEarliestEvent()

		if earliest == nil || earliest.After(target) {
			c.now = target
			break
		}

		c.now = *earliest
		c.fireTickers()
		c.fireWaiters()
	}

	select {
	case c.advanced <- struct{}{}:
	default:
	}
}

func (c *FakeClock) findEarliestEvent() *time.Time {
	var earliest *time.Time

	for _, t := range c.tickers {
		if t.stopped {
			continue
		}
		if earliest == nil || t.nextTick.Before(*earliest) {
			tick := t.nextTick
			earliest = &tick
		}
	}

	for _, w := range c.waiters {
		if earliest == nil || w.deadline.Before(*earliest) {
			d := w.deadline
			earliest = &d
		}
	}

	return earliest
}

func (c *FakeClock) fireTickers() {
	for _, t := range c.tickers {
		if t.stopped || t.nextTick.After(c.now) {
			continue
		}
		select {
		case t.ch <- c.now:
		default:
		}
		t.nextTick = c.now.Add(t.duration)
	}
}

func (c *FakeClock) fireWaiters() {
	remaining := make([]waiter, 0, len(c.waiters))
	for _, w := range c.waiters {
		if !w.deadline.After(c.now) {
			select {
			case w.ch <- c.now:
			default:
			}
		} else {
			remaining = append(remaining, w)
		}
	}
	c.waiters = remaining
}

func (c *FakeClock) WaitForAdvance() {
	<-c.advanced
}

func (c *FakeClock) TickerCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	count := 0
	for _, t := range c.tickers {
		if !t.stopped {
			count++
		}
	}
	return count
}

type fakeTicker struct {
	clock    *FakeClock
	duration time.Duration
	ch       chan time.Time
	nextTick time.Time
	stopped  bool
}

func (t *fakeTicker) C() <-chan time.Time {
	return t.ch
}

func (t *fakeTicker) Stop() {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	t.stopped = true
}
