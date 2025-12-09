package middlewares

import (
	"errors"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/netresearch/ofelia/core"
)

type SuiteDedup struct {
	BaseSuite
}

var _ = Suite(&SuiteDedup{})

func (s *SuiteDedup) TestNewNotificationDedup(c *C) {
	dedup := NewNotificationDedup(time.Hour)
	c.Assert(dedup, NotNil)
	c.Assert(dedup.cooldown, Equals, time.Hour)
	c.Assert(dedup.entries, NotNil)
}

func (s *SuiteDedup) TestGenerateKey(c *C) {
	dedup := NewNotificationDedup(time.Hour)

	// Set up job with name and command
	s.job.Name = "test-job"
	s.job.Command = "echo hello"

	s.ctx.Start()
	s.ctx.Stop(errors.New("connection refused"))

	key := dedup.generateKey(s.ctx)
	c.Assert(key, Not(Equals), "")

	// Same error should produce same key
	s.ctx.Start()
	s.ctx.Stop(errors.New("connection refused"))
	key2 := dedup.generateKey(s.ctx)
	c.Assert(key, Equals, key2)

	// Different error should produce different key
	s.ctx.Start()
	s.ctx.Stop(errors.New("timeout"))
	key3 := dedup.generateKey(s.ctx)
	c.Assert(key, Not(Equals), key3)
}

func (s *SuiteDedup) TestShouldNotify_FirstError(c *C) {
	dedup := NewNotificationDedup(time.Hour)

	s.job.Name = "test-job"
	s.job.Command = "echo hello"

	s.ctx.Start()
	s.ctx.Stop(errors.New("first error"))

	// First occurrence should always notify
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, true)
}

func (s *SuiteDedup) TestShouldNotify_DuplicateWithinCooldown(c *C) {
	dedup := NewNotificationDedup(time.Hour)

	s.job.Name = "test-job"
	s.job.Command = "echo hello"

	// First error - should notify
	s.ctx.Start()
	s.ctx.Stop(errors.New("same error"))
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, true)

	// Same error again immediately - should NOT notify (within cooldown)
	s.ctx.Start()
	s.ctx.Stop(errors.New("same error"))
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, false)
}

func (s *SuiteDedup) TestShouldNotify_DifferentErrors(c *C) {
	dedup := NewNotificationDedup(time.Hour)

	s.job.Name = "test-job"
	s.job.Command = "echo hello"

	// First error
	s.ctx.Start()
	s.ctx.Stop(errors.New("error A"))
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, true)

	// Different error - should notify
	s.ctx.Start()
	s.ctx.Stop(errors.New("error B"))
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, true)
}

func (s *SuiteDedup) TestShouldNotify_AfterCooldownExpires(c *C) {
	// Use very short cooldown for testing
	dedup := NewNotificationDedup(10 * time.Millisecond)

	s.job.Name = "test-job"
	s.job.Command = "echo hello"

	// First error - should notify
	s.ctx.Start()
	s.ctx.Stop(errors.New("same error"))
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, true)

	// Same error immediately - should NOT notify
	s.ctx.Start()
	s.ctx.Stop(errors.New("same error"))
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, false)

	// Wait for cooldown to expire
	time.Sleep(15 * time.Millisecond)

	// Same error after cooldown - should notify again
	s.ctx.Start()
	s.ctx.Stop(errors.New("same error"))
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, true)
}

func (s *SuiteDedup) TestShouldNotify_SuccessAlwaysNotifies(c *C) {
	dedup := NewNotificationDedup(time.Hour)

	s.job.Name = "test-job"
	s.job.Command = "echo hello"

	// Success - should always notify (no dedup for success)
	s.ctx.Start()
	s.ctx.Stop(nil)
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, true)

	// Another success - should also notify
	s.ctx.Start()
	s.ctx.Stop(nil)
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, true)
}

func (s *SuiteDedup) TestShouldNotify_DifferentJobs(c *C) {
	dedup := NewNotificationDedup(time.Hour)

	// Job 1 fails
	s.job.Name = "job-1"
	s.job.Command = "echo hello"
	s.ctx.Start()
	s.ctx.Stop(errors.New("same error"))
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, true)

	// Job 2 fails with same error - should still notify (different job)
	s.job.Name = "job-2"
	s.ctx.Start()
	s.ctx.Stop(errors.New("same error"))
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, true)
}

func (s *SuiteDedup) TestCleanup_RemovesExpiredEntries(c *C) {
	dedup := NewNotificationDedup(10 * time.Millisecond)

	s.job.Name = "test-job"
	s.job.Command = "echo hello"

	// Add some entries
	s.ctx.Start()
	s.ctx.Stop(errors.New("error 1"))
	dedup.ShouldNotify(s.ctx)

	s.ctx.Start()
	s.ctx.Stop(errors.New("error 2"))
	dedup.ShouldNotify(s.ctx)

	c.Assert(len(dedup.entries), Equals, 2)

	// Wait for cooldown to expire
	time.Sleep(15 * time.Millisecond)

	// Cleanup should remove expired entries
	dedup.Cleanup()
	c.Assert(len(dedup.entries), Equals, 0)
}

func (s *SuiteDedup) TestConcurrentAccess(c *C) {
	dedup := NewNotificationDedup(time.Hour)

	s.job.Name = "test-job"
	s.job.Command = "echo hello"

	// Simulate concurrent access
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(errNum int) {
			ctx := s.createContext(c)
			ctx.Start()
			ctx.Stop(errors.New("error"))
			dedup.ShouldNotify(ctx)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic and should have reasonable state
	c.Assert(len(dedup.entries) >= 1, Equals, true)
}

func (s *SuiteDedup) createContext(c *C) *core.Context {
	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	c.Assert(err, IsNil)
	return core.NewContext(sh, s.job, e)
}

// Test disabled dedup (zero cooldown)
func (s *SuiteDedup) TestZeroCooldown_AlwaysNotifies(c *C) {
	dedup := NewNotificationDedup(0)

	s.job.Name = "test-job"
	s.job.Command = "echo hello"

	// With zero cooldown, should always notify
	s.ctx.Start()
	s.ctx.Stop(errors.New("same error"))
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, true)

	s.ctx.Start()
	s.ctx.Stop(errors.New("same error"))
	c.Assert(dedup.ShouldNotify(s.ctx), Equals, true)
}

// Integration test with standard Go testing for better IDE support
func TestNotificationDedup_Integration(t *testing.T) {
	dedup := NewNotificationDedup(100 * time.Millisecond)

	// Create a mock context
	job := &TestJob{}
	job.Name = "integration-test-job"
	job.Command = "test command"

	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := core.NewContext(sh, job, e)

	// Test sequence: notify -> suppress -> notify after cooldown
	ctx.Start()
	ctx.Stop(errors.New("test error"))

	if !dedup.ShouldNotify(ctx) {
		t.Error("Expected first notification to be allowed")
	}

	ctx.Start()
	ctx.Stop(errors.New("test error"))

	if dedup.ShouldNotify(ctx) {
		t.Error("Expected duplicate notification to be suppressed")
	}

	// Wait for cooldown
	time.Sleep(150 * time.Millisecond)

	ctx.Start()
	ctx.Stop(errors.New("test error"))

	if !dedup.ShouldNotify(ctx) {
		t.Error("Expected notification after cooldown to be allowed")
	}
}
