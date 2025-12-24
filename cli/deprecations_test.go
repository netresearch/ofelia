package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/netresearch/ofelia/middlewares"
)

func TestDeprecationRegistry_Check_SlackWebhook(t *testing.T) {
	t.Parallel()

	// Test: no slack webhook
	t.Run("no slack webhook", func(t *testing.T) {
		cfg := &Config{}
		registry := &DeprecationRegistry{warnings: make(map[string]bool)}
		found := registry.ForDoctor(cfg)

		hasSlackDeprecation := false
		for _, dep := range found {
			if dep.Option == "slack-webhook" {
				hasSlackDeprecation = true
				break
			}
		}
		assert.False(t, hasSlackDeprecation)
	})

	// Test: global slack webhook
	t.Run("global slack webhook", func(t *testing.T) {
		cfg := &Config{}
		cfg.Global.SlackConfig.SlackWebhook = "https://hooks.slack.com/test"

		registry := &DeprecationRegistry{warnings: make(map[string]bool)}
		found := registry.ForDoctor(cfg)

		hasSlackDeprecation := false
		for _, dep := range found {
			if dep.Option == "slack-webhook" {
				hasSlackDeprecation = true
				break
			}
		}
		assert.True(t, hasSlackDeprecation)
	})

	// Test: exec job slack webhook
	t.Run("exec job slack webhook", func(t *testing.T) {
		cfg := &Config{
			ExecJobs: map[string]*ExecJobConfig{
				"test": {SlackConfig: middlewares.SlackConfig{SlackWebhook: "https://hooks.slack.com/test"}},
			},
		}

		registry := &DeprecationRegistry{warnings: make(map[string]bool)}
		found := registry.ForDoctor(cfg)

		hasSlackDeprecation := false
		for _, dep := range found {
			if dep.Option == "slack-webhook" {
				hasSlackDeprecation = true
				break
			}
		}
		assert.True(t, hasSlackDeprecation)
	})

	// Test: run job slack webhook
	t.Run("run job slack webhook", func(t *testing.T) {
		cfg := &Config{
			RunJobs: map[string]*RunJobConfig{
				"test": {SlackConfig: middlewares.SlackConfig{SlackWebhook: "https://hooks.slack.com/test"}},
			},
		}

		registry := &DeprecationRegistry{warnings: make(map[string]bool)}
		found := registry.ForDoctor(cfg)

		hasSlackDeprecation := false
		for _, dep := range found {
			if dep.Option == "slack-webhook" {
				hasSlackDeprecation = true
				break
			}
		}
		assert.True(t, hasSlackDeprecation)
	})
}

func TestDeprecationRegistry_Check_PollInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      *Config
		expected bool
	}{
		{
			name: "no poll-interval",
			cfg: &Config{
				Docker: DockerConfig{
					PollInterval: 0,
				},
			},
			expected: false,
		},
		{
			name: "poll-interval set",
			cfg: &Config{
				Docker: DockerConfig{
					PollInterval: 30 * time.Second,
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := &DeprecationRegistry{warnings: make(map[string]bool)}
			found := registry.ForDoctor(tt.cfg)

			hasPollIntervalDeprecation := false
			for _, dep := range found {
				if dep.Option == "poll-interval" {
					hasPollIntervalDeprecation = true
					break
				}
			}
			assert.Equal(t, tt.expected, hasPollIntervalDeprecation)
		})
	}
}

func TestDeprecationRegistry_Check_NoPoll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      *Config
		expected bool
	}{
		{
			name: "no-poll not set",
			cfg: &Config{
				Docker: DockerConfig{
					DisablePolling: false,
				},
			},
			expected: false,
		},
		{
			name: "no-poll set",
			cfg: &Config{
				Docker: DockerConfig{
					DisablePolling: true,
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := &DeprecationRegistry{warnings: make(map[string]bool)}
			found := registry.ForDoctor(tt.cfg)

			hasNoPollDeprecation := false
			for _, dep := range found {
				if dep.Option == "no-poll" {
					hasNoPollDeprecation = true
					break
				}
			}
			assert.Equal(t, tt.expected, hasNoPollDeprecation)
		})
	}
}

func TestDeprecationRegistry_Reset(t *testing.T) {
	t.Parallel()

	registry := &DeprecationRegistry{warnings: make(map[string]bool)}

	// Mark some warnings as shown
	registry.warnings["test-option"] = true
	registry.warnings["another-option"] = true

	assert.Len(t, registry.warnings, 2)

	// Reset should clear all warnings
	registry.Reset()

	assert.Empty(t, registry.warnings)
}

func TestDeprecationRegistry_WarningsOncePerCycle(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Docker: DockerConfig{
			PollInterval: 30 * time.Second,
		},
	}

	registry := &DeprecationRegistry{warnings: make(map[string]bool)}

	// First check should add to warnings
	found1 := registry.Check(cfg)
	assert.Len(t, found1, 1)
	assert.True(t, registry.warnings["poll-interval"])

	// Second check with same config should still find deprecation but not warn again
	found2 := registry.Check(cfg)
	assert.Len(t, found2, 1)

	// Reset and check again - should warn again
	registry.Reset()
	assert.Empty(t, registry.warnings)

	found3 := registry.Check(cfg)
	assert.Len(t, found3, 1)
	assert.True(t, registry.warnings["poll-interval"])
}

func TestApplyDeprecationMigrations_PollInterval(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Docker: DockerConfig{
			PollInterval:       30 * time.Second,
			ConfigPollInterval: 10 * time.Second, // default
			DockerPollInterval: 0,
			PollingFallback:    10 * time.Second, // default
			UseEvents:          false,
		},
	}

	ApplyDeprecationMigrations(cfg)

	// Values should be migrated
	assert.Equal(t, 30*time.Second, cfg.Docker.ConfigPollInterval)
	assert.Equal(t, 30*time.Second, cfg.Docker.DockerPollInterval)
	assert.Equal(t, 30*time.Second, cfg.Docker.PollingFallback)
}

func TestApplyDeprecationMigrations_PollIntervalExplicitOverride(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Docker: DockerConfig{
			PollInterval:       30 * time.Second,
			ConfigPollInterval: 20 * time.Second, // explicit, not default
			DockerPollInterval: 15 * time.Second, // explicit
			PollingFallback:    5 * time.Second,  // explicit, not default
			UseEvents:          true,
		},
	}

	ApplyDeprecationMigrations(cfg)

	// Explicit values should NOT be overwritten
	assert.Equal(t, 20*time.Second, cfg.Docker.ConfigPollInterval)
	assert.Equal(t, 15*time.Second, cfg.Docker.DockerPollInterval)
	assert.Equal(t, 5*time.Second, cfg.Docker.PollingFallback)
}

func TestApplyDeprecationMigrations_NoPoll(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Docker: DockerConfig{
			DisablePolling:     true,
			DockerPollInterval: 30 * time.Second,
			PollingFallback:    10 * time.Second,
		},
	}

	ApplyDeprecationMigrations(cfg)

	// no-poll should disable docker polling and fallback
	assert.Equal(t, time.Duration(0), cfg.Docker.DockerPollInterval)
	assert.Equal(t, time.Duration(0), cfg.Docker.PollingFallback)
}

func TestDeprecations_AllHaveRequiredFields(t *testing.T) {
	t.Parallel()

	for _, dep := range Deprecations {
		assert.NotEmpty(t, dep.Option, "Deprecation must have Option")
		assert.NotEmpty(t, dep.Replacement, "Deprecation must have Replacement")
		assert.NotEmpty(t, dep.RemovalVersion, "Deprecation must have RemovalVersion")
		assert.NotNil(t, dep.CheckFunc, "Deprecation must have CheckFunc")
		// MigrateFunc is optional (slack-webhook doesn't need one)
	}
}

func TestDeprecations_RemovalVersionFormat(t *testing.T) {
	t.Parallel()

	for _, dep := range Deprecations {
		// All deprecations should target v1.0.0
		assert.Equal(t, "v1.0.0", dep.RemovalVersion,
			"Deprecation %s should target v1.0.0", dep.Option)
	}
}

func TestDeprecationRegistry_CheckWithKeys_PresenceBasedDetection(t *testing.T) {
	t.Parallel()

	// Test that poll-interval is detected by key presence, even when value is 0
	t.Run("poll-interval detected by presence when value is 0", func(t *testing.T) {
		cfg := &Config{
			Docker: DockerConfig{
				PollInterval: 0, // Zero value!
			},
		}

		// Without key presence info, value-based detection returns false
		registry := &DeprecationRegistry{warnings: make(map[string]bool)}
		foundWithoutKeys := registry.ForDoctor(cfg)

		hasPollIntervalWithoutKeys := false
		for _, dep := range foundWithoutKeys {
			if dep.Option == "poll-interval" {
				hasPollIntervalWithoutKeys = true
				break
			}
		}
		assert.False(t, hasPollIntervalWithoutKeys, "Value-based detection should not find poll-interval=0")

		// With key presence info, it should be detected
		usedKeys := map[string]bool{"pollinterval": true}
		foundWithKeys := registry.ForDoctorWithKeys(cfg, usedKeys)

		hasPollIntervalWithKeys := false
		for _, dep := range foundWithKeys {
			if dep.Option == "poll-interval" {
				hasPollIntervalWithKeys = true
				break
			}
		}
		assert.True(t, hasPollIntervalWithKeys, "Key-presence detection should find poll-interval even when 0")
	})

	// Test that no-poll is detected by key presence, even when value is false
	t.Run("no-poll detected by presence when value is false", func(t *testing.T) {
		cfg := &Config{
			Docker: DockerConfig{
				DisablePolling: false, // Zero value!
			},
		}

		// Without key presence info, value-based detection returns false
		registry := &DeprecationRegistry{warnings: make(map[string]bool)}
		foundWithoutKeys := registry.ForDoctor(cfg)

		hasNoPollWithoutKeys := false
		for _, dep := range foundWithoutKeys {
			if dep.Option == "no-poll" {
				hasNoPollWithoutKeys = true
				break
			}
		}
		assert.False(t, hasNoPollWithoutKeys, "Value-based detection should not find no-poll=false")

		// With key presence info, it should be detected
		usedKeys := map[string]bool{"nopoll": true}
		foundWithKeys := registry.ForDoctorWithKeys(cfg, usedKeys)

		hasNoPollWithKeys := false
		for _, dep := range foundWithKeys {
			if dep.Option == "no-poll" {
				hasNoPollWithKeys = true
				break
			}
		}
		assert.True(t, hasNoPollWithKeys, "Key-presence detection should find no-poll even when false")
	})
}

func TestApplyDeprecationMigrationsWithKeys_PresenceBasedMigration(t *testing.T) {
	t.Parallel()

	// Key presence triggers migration to be *called*, but the migration logic itself
	// still respects the actual value. This test verifies migration is called when
	// key is present, even if value-based CheckFunc would return false.
	t.Run("no-poll migration called when key present and value true", func(t *testing.T) {
		cfg := &Config{
			Docker: DockerConfig{
				DisablePolling:     true, // Migration will actually change values
				DockerPollInterval: 30 * time.Second,
				PollingFallback:    10 * time.Second,
			},
		}

		// With key presence and value=true, migration should trigger
		usedKeys := map[string]bool{"nopoll": true}
		ApplyDeprecationMigrationsWithKeys(cfg, usedKeys)
		assert.Equal(t, time.Duration(0), cfg.Docker.DockerPollInterval, "Migration should trigger")
		assert.Equal(t, time.Duration(0), cfg.Docker.PollingFallback, "Migration should trigger")
	})

	// The key difference from value-based detection: the migration is *called* based
	// on key presence, but what it does depends on the actual value.
	t.Run("poll-interval migration respects value even with key presence", func(t *testing.T) {
		cfg := &Config{
			Docker: DockerConfig{
				PollInterval:       0, // Zero value - migration function will return early
				ConfigPollInterval: 10 * time.Second,
				DockerPollInterval: 0,
				PollingFallback:    10 * time.Second,
				UseEvents:          false,
			},
		}

		// Even with key presence, if value is 0, the migration function returns early
		usedKeys := map[string]bool{"pollinterval": true}
		ApplyDeprecationMigrationsWithKeys(cfg, usedKeys)

		// Values remain unchanged because PollInterval is 0
		assert.Equal(t, 10*time.Second, cfg.Docker.ConfigPollInterval)
	})
}

func TestDeprecations_KeyNameSet(t *testing.T) {
	t.Parallel()

	// Verify that poll-interval and no-poll have KeyName set for presence-based detection
	foundPollInterval := false
	foundNoPoll := false

	for _, dep := range Deprecations {
		switch dep.Option {
		case "poll-interval":
			assert.NotEmpty(t, dep.KeyName, "poll-interval should have KeyName set")
			assert.Equal(t, "pollinterval", dep.KeyName)
			foundPollInterval = true
		case "no-poll":
			assert.NotEmpty(t, dep.KeyName, "no-poll should have KeyName set")
			assert.Equal(t, "nopoll", dep.KeyName)
			foundNoPoll = true
		}
	}

	assert.True(t, foundPollInterval, "poll-interval deprecation should exist")
	assert.True(t, foundNoPoll, "no-poll deprecation should exist")
}
