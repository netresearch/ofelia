package middlewares

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

func TestNewNotificationDedup(t *testing.T) {
	t.Parallel()

	dedup := NewNotificationDedup(time.Hour)
	assert.NotNil(t, dedup)
	assert.Equal(t, time.Hour, dedup.cooldown)
	assert.NotNil(t, dedup.entries)
}

func TestDedupGenerateKey(t *testing.T) {
	t.Parallel()
	ctx, job := setupTestContext(t)

	dedup := NewNotificationDedup(time.Hour)

	// Set up job with name and command
	job.Name = "test-job"
	job.Command = "echo hello"

	ctx.Start()
	ctx.Stop(errors.New("connection refused"))

	key := dedup.generateKey(ctx)
	assert.NotEmpty(t, key)

	// Same error should produce same key
	ctx.Start()
	ctx.Stop(errors.New("connection refused"))
	key2 := dedup.generateKey(ctx)
	assert.Equal(t, key, key2)

	// Different error should produce different key
	ctx.Start()
	ctx.Stop(errors.New("timeout"))
	key3 := dedup.generateKey(ctx)
	assert.NotEqual(t, key, key3)
}

func TestShouldNotify_FirstError(t *testing.T) {
	t.Parallel()
	ctx, job := setupTestContext(t)

	dedup := NewNotificationDedup(time.Hour)

	job.Name = "test-job"
	job.Command = "echo hello"

	ctx.Start()
	ctx.Stop(errors.New("first error"))

	// First occurrence should always notify
	assert.True(t, dedup.ShouldNotify(ctx))
}

func TestShouldNotify_DuplicateWithinCooldown(t *testing.T) {
	t.Parallel()
	ctx, job := setupTestContext(t)

	dedup := NewNotificationDedup(time.Hour)

	job.Name = "test-job"
	job.Command = "echo hello"

	// First error - should notify
	ctx.Start()
	ctx.Stop(errors.New("same error"))
	assert.True(t, dedup.ShouldNotify(ctx))

	// Same error again immediately - should NOT notify (within cooldown)
	ctx.Start()
	ctx.Stop(errors.New("same error"))
	assert.False(t, dedup.ShouldNotify(ctx))
}

func TestShouldNotify_DifferentErrors(t *testing.T) {
	t.Parallel()
	ctx, job := setupTestContext(t)

	dedup := NewNotificationDedup(time.Hour)

	job.Name = "test-job"
	job.Command = "echo hello"

	// First error
	ctx.Start()
	ctx.Stop(errors.New("error A"))
	assert.True(t, dedup.ShouldNotify(ctx))

	// Different error - should notify
	ctx.Start()
	ctx.Stop(errors.New("error B"))
	assert.True(t, dedup.ShouldNotify(ctx))
}

func TestShouldNotify_AfterCooldownExpires(t *testing.T) {
	t.Parallel()
	ctx, job := setupTestContext(t)

	// Use very short cooldown for testing
	dedup := NewNotificationDedup(10 * time.Millisecond)

	job.Name = "test-job"
	job.Command = "echo hello"

	// First error - should notify
	ctx.Start()
	ctx.Stop(errors.New("same error"))
	assert.True(t, dedup.ShouldNotify(ctx))

	// Same error immediately - should NOT notify
	ctx.Start()
	ctx.Stop(errors.New("same error"))
	assert.False(t, dedup.ShouldNotify(ctx))

	// Wait for cooldown to expire
	time.Sleep(15 * time.Millisecond)

	// Same error after cooldown - should notify again
	ctx.Start()
	ctx.Stop(errors.New("same error"))
	assert.True(t, dedup.ShouldNotify(ctx))
}

func TestShouldNotify_SuccessAlwaysNotifies(t *testing.T) {
	t.Parallel()
	ctx, job := setupTestContext(t)

	dedup := NewNotificationDedup(time.Hour)

	job.Name = "test-job"
	job.Command = "echo hello"

	// Success - should always notify (no dedup for success)
	ctx.Start()
	ctx.Stop(nil)
	assert.True(t, dedup.ShouldNotify(ctx))

	// Another success - should also notify
	ctx.Start()
	ctx.Stop(nil)
	assert.True(t, dedup.ShouldNotify(ctx))
}

func TestShouldNotify_DifferentJobs(t *testing.T) {
	t.Parallel()
	ctx, job := setupTestContext(t)

	dedup := NewNotificationDedup(time.Hour)

	// Job 1 fails
	job.Name = "job-1"
	job.Command = "echo hello"
	ctx.Start()
	ctx.Stop(errors.New("same error"))
	assert.True(t, dedup.ShouldNotify(ctx))

	// Job 2 fails with same error - should still notify (different job)
	job.Name = "job-2"
	ctx.Start()
	ctx.Stop(errors.New("same error"))
	assert.True(t, dedup.ShouldNotify(ctx))
}

func TestCleanup_RemovesExpiredEntries(t *testing.T) {
	t.Parallel()
	ctx, job := setupTestContext(t)

	dedup := NewNotificationDedup(10 * time.Millisecond)

	job.Name = "test-job"
	job.Command = "echo hello"

	// Add some entries
	ctx.Start()
	ctx.Stop(errors.New("error 1"))
	dedup.ShouldNotify(ctx)

	ctx.Start()
	ctx.Stop(errors.New("error 2"))
	dedup.ShouldNotify(ctx)

	assert.Len(t, dedup.entries, 2)

	// Wait for cooldown to expire
	time.Sleep(15 * time.Millisecond)

	// Cleanup should remove expired entries
	dedup.Cleanup()
	assert.Empty(t, dedup.entries)
}

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()
	_, job := setupTestContext(t)

	dedup := NewNotificationDedup(time.Hour)

	job.Name = "test-job"
	job.Command = "echo hello"

	// Simulate concurrent access
	done := make(chan bool, 10)
	for range 10 {
		go func() {
			sh := core.NewScheduler(&TestLogger{})
			e, err := core.NewExecution()
			assert.NoError(t, err) // use assert in goroutines, not require
			ctx := core.NewContext(sh, job, e)
			ctx.Start()
			ctx.Stop(errors.New("error"))
			dedup.ShouldNotify(ctx)
			done <- true
		}()
	}

	// Wait for all goroutines
	for range 10 {
		<-done
	}

	// Should not panic and should have reasonable state
	assert.GreaterOrEqual(t, len(dedup.entries), 1)
}

// Test disabled dedup (zero cooldown)
func TestZeroCooldown_AlwaysNotifies(t *testing.T) {
	t.Parallel()
	ctx, job := setupTestContext(t)

	dedup := NewNotificationDedup(0)

	job.Name = "test-job"
	job.Command = "echo hello"

	// With zero cooldown, should always notify
	ctx.Start()
	ctx.Stop(errors.New("same error"))
	assert.True(t, dedup.ShouldNotify(ctx))

	ctx.Start()
	ctx.Stop(errors.New("same error"))
	assert.True(t, dedup.ShouldNotify(ctx))
}

// Integration test with standard Go testing for better IDE support
func TestNotificationDedup_Integration(t *testing.T) {
	t.Parallel()

	dedup := NewNotificationDedup(100 * time.Millisecond)

	// Create a mock context
	job := &TestJob{}
	job.Name = "integration-test-job"
	job.Command = "test command"

	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	require.NoError(t, err)

	ctx := core.NewContext(sh, job, e)

	// Test sequence: notify -> suppress -> notify after cooldown
	ctx.Start()
	ctx.Stop(errors.New("test error"))

	assert.True(t, dedup.ShouldNotify(ctx), "Expected first notification to be allowed")

	ctx.Start()
	ctx.Stop(errors.New("test error"))

	assert.False(t, dedup.ShouldNotify(ctx), "Expected duplicate notification to be suppressed")

	// Wait for cooldown
	time.Sleep(150 * time.Millisecond)

	ctx.Start()
	ctx.Stop(errors.New("test error"))

	assert.True(t, dedup.ShouldNotify(ctx), "Expected notification after cooldown to be allowed")
}
