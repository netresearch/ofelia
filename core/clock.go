package core

import (
	"sync"
	"time"

	cron "github.com/netresearch/go-cron"
)

type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
	NewTimer(d time.Duration) Timer
	After(d time.Duration) <-chan time.Time
	Sleep(d time.Duration)
}

// Timer represents a single event timer, compatible with go-cron's Timer interface.
// It provides the same operations as time.Timer.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
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

func (c *realClock) NewTimer(d time.Duration) Timer {
	return &realTimer{timer: time.NewTimer(d)}
}

type realTimer struct {
	timer *time.Timer
}

func (t *realTimer) C() <-chan time.Time {
	return t.timer.C
}

func (t *realTimer) Stop() bool {
	return t.timer.Stop()
}

func (t *realTimer) Reset(d time.Duration) bool {
	return t.timer.Reset(d)
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
	timers   []*fakeTimer
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

func (c *FakeClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()

	ft := &fakeTimer{
		clock:    c,
		ch:       make(chan time.Time, 1),
		deadline: c.now.Add(d),
	}
	c.timers = append(c.timers, ft)
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
		c.fireTimers()
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

	for _, t := range c.timers {
		if t.stopped || t.fired {
			continue
		}
		if earliest == nil || t.deadline.Before(*earliest) {
			d := t.deadline
			earliest = &d
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

func (c *FakeClock) fireTimers() {
	for _, t := range c.timers {
		if t.stopped || t.fired || t.deadline.After(c.now) {
			continue
		}
		select {
		case t.ch <- c.now:
		default:
		}
		t.fired = true
	}
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

type fakeTimer struct {
	clock    *FakeClock
	ch       chan time.Time
	deadline time.Time
	stopped  bool
	fired    bool
}

func (t *fakeTimer) C() <-chan time.Time {
	return t.ch
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	wasActive := !t.stopped && !t.fired
	t.stopped = true
	return wasActive
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	wasActive := !t.stopped && !t.fired
	t.stopped = false
	t.fired = false
	t.deadline = t.clock.now.Add(d)
	return wasActive
}

type CronClock struct {
	*FakeClock
}

func NewCronClock(start time.Time) *CronClock {
	return &CronClock{FakeClock: NewFakeClock(start)}
}

func (c *CronClock) NewTimer(d time.Duration) cron.Timer {
	return c.FakeClock.NewTimer(d)
}
