package middlewares

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/netresearch/ofelia/core"
)

// NotificationDedup provides deduplication of error notifications.
// It tracks recent error notifications and suppresses duplicates within
// a configurable cooldown period to prevent notification spam.
type NotificationDedup struct {
	cooldown time.Duration
	entries  map[string]time.Time
	mu       sync.RWMutex
}

// NewNotificationDedup creates a new notification deduplicator with the
// specified cooldown period. If cooldown is 0, deduplication is disabled
// and all notifications are allowed.
func NewNotificationDedup(cooldown time.Duration) *NotificationDedup {
	return &NotificationDedup{
		cooldown: cooldown,
		entries:  make(map[string]time.Time),
	}
}

// ShouldNotify returns true if the notification should be sent, false if it
// should be suppressed as a duplicate. Successful executions always return
// true (no deduplication for success). Failed executions are deduplicated
// based on job name, command, and error message.
func (d *NotificationDedup) ShouldNotify(ctx *core.Context) bool {
	// Disabled dedup - always notify
	if d.cooldown == 0 {
		return true
	}

	// Always notify for successful executions
	if !ctx.Execution.Failed {
		return true
	}

	key := d.generateKey(ctx)

	d.mu.Lock()
	defer d.mu.Unlock()

	lastNotified, exists := d.entries[key]
	now := time.Now()

	// First occurrence or cooldown expired
	if !exists || now.Sub(lastNotified) >= d.cooldown {
		d.entries[key] = now
		return true
	}

	// Within cooldown period - suppress notification
	return false
}

// generateKey creates a unique key for deduplication based on job name,
// command, and error message. This ensures that different errors from
// the same job or same errors from different jobs are tracked separately.
func (d *NotificationDedup) generateKey(ctx *core.Context) string {
	h := sha256.New()
	h.Write([]byte(ctx.Job.GetName()))
	h.Write([]byte(ctx.Job.GetCommand()))

	if ctx.Execution.Error != nil {
		h.Write([]byte(ctx.Execution.Error.Error()))
	}

	return hex.EncodeToString(h.Sum(nil))
}

// Cleanup removes expired entries from the deduplication map.
// This should be called periodically to prevent memory leaks for
// jobs that no longer fail.
func (d *NotificationDedup) Cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for key, lastNotified := range d.entries {
		if now.Sub(lastNotified) >= d.cooldown {
			delete(d.entries, key)
		}
	}
}

// Len returns the number of entries in the deduplication map.
// Useful for testing and monitoring.
func (d *NotificationDedup) Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.entries)
}

// DefaultNotificationDedup is the global deduplicator instance used by
// notification middlewares. It's initialized when configuration is loaded.
var DefaultNotificationDedup *NotificationDedup

// InitNotificationDedup initializes the global deduplicator with the
// specified cooldown period. Call this during configuration loading.
func InitNotificationDedup(cooldown time.Duration) {
	DefaultNotificationDedup = NewNotificationDedup(cooldown)
}

// StartCleanupRoutine starts a background goroutine that periodically
// cleans up expired entries. Returns a stop function to cancel the routine.
func (d *NotificationDedup) StartCleanupRoutine(interval time.Duration) func() {
	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				d.Cleanup()
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	return func() {
		close(done)
	}
}
