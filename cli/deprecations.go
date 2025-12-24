package cli

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/netresearch/ofelia/core"
)

// Deprecation defines a deprecated configuration option
type Deprecation struct {
	// Option is the deprecated option name (e.g., "slack-webhook", "poll-interval")
	Option string

	// Replacement describes what to use instead
	Replacement string

	// RemovalVersion is the version when this option will be removed
	RemovalVersion string

	// Message is an optional additional message with migration instructions
	Message string

	// KeyName is the configuration key name for presence-based detection.
	// When set, the deprecation is detected if the key is present in the config,
	// regardless of its value (even if set to zero/empty).
	// This is the normalized key name (lowercase, no separators).
	KeyName string

	// CheckFunc returns true if the deprecated option is in use
	// The function receives the global config and checks if the deprecated option is set
	// This is the fallback when KeyName is not set or usedKeys is not available.
	CheckFunc func(cfg *Config) bool

	// MigrateFunc applies backwards-compatible migration from deprecated to new options
	// This is called during config loading to ensure deprecated options still work
	MigrateFunc func(cfg *Config)
}

// DeprecationRegistry tracks deprecated options and ensures warnings are shown once per config load
type DeprecationRegistry struct {
	mu       sync.Mutex
	warnings map[string]bool // tracks what's been warned this cycle
	logger   core.Logger
}

// Global deprecation registry instance
var deprecationRegistry = &DeprecationRegistry{
	warnings: make(map[string]bool),
}

// Deprecations is the central list of all deprecated configuration options
var Deprecations = []Deprecation{
	{
		Option:         "slack-webhook",
		Replacement:    "[webhook \"name\"] sections with preset=slack",
		RemovalVersion: "v1.0.0",
		Message: `Please migrate to the new webhook notification system:
  [webhook "slack"]
  preset = slack
  id = T.../B...
  secret = XXXX...

See documentation: https://github.com/netresearch/ofelia#webhook-notifications`,
		CheckFunc: func(cfg *Config) bool {
			// Check global slack config
			if cfg.Global.SlackWebhook != "" {
				return true
			}
			// Check per-job slack configs
			for _, job := range cfg.ExecJobs {
				if job.SlackWebhook != "" {
					return true
				}
			}
			for _, job := range cfg.RunJobs {
				if job.SlackWebhook != "" {
					return true
				}
			}
			for _, job := range cfg.LocalJobs {
				if job.SlackWebhook != "" {
					return true
				}
			}
			for _, job := range cfg.ComposeJobs {
				if job.SlackWebhook != "" {
					return true
				}
			}
			for _, job := range cfg.ServiceJobs {
				if job.SlackWebhook != "" {
					return true
				}
			}
			return false
		},
		// No migration needed - slack middleware reads the deprecated field directly
		MigrateFunc: nil,
	},
	{
		Option:         "poll-interval",
		Replacement:    "config-poll-interval and docker-poll-interval",
		RemovalVersion: "v1.0.0",
		Message:        "Use 'config-poll-interval' for INI file watching and 'docker-poll-interval' for container polling fallback.",
		KeyName:        "pollinterval", // Normalized key for presence-based detection
		CheckFunc: func(cfg *Config) bool {
			return cfg.Docker.PollInterval > 0
		},
		MigrateFunc: func(cfg *Config) {
			if cfg.Docker.PollInterval <= 0 {
				return
			}
			// If new options aren't explicitly set, use deprecated value
			if cfg.Docker.ConfigPollInterval == 10*time.Second { // default value
				cfg.Docker.ConfigPollInterval = cfg.Docker.PollInterval
			}
			// For BC: if events are disabled and poll-interval was set, enable container polling
			if !cfg.Docker.UseEvents && cfg.Docker.DockerPollInterval == 0 {
				cfg.Docker.DockerPollInterval = cfg.Docker.PollInterval
			}
			// For BC: use poll-interval as polling-fallback if fallback wasn't explicitly set
			if cfg.Docker.PollingFallback == 10*time.Second { // default value
				cfg.Docker.PollingFallback = cfg.Docker.PollInterval
			}
		},
	},
	{
		Option:         "no-poll",
		Replacement:    "docker-poll-interval=0",
		RemovalVersion: "v1.0.0",
		Message:        "Use 'docker-poll-interval=0' to disable container polling.",
		KeyName:        "nopoll", // Normalized key for presence-based detection
		CheckFunc: func(cfg *Config) bool {
			return cfg.Docker.DisablePolling
		},
		MigrateFunc: func(cfg *Config) {
			if !cfg.Docker.DisablePolling {
				return
			}
			cfg.Docker.DockerPollInterval = 0
			cfg.Docker.PollingFallback = 0 // Also disable fallback
		},
	},
}

// SetLogger sets the logger for deprecation warnings
func (r *DeprecationRegistry) SetLogger(logger core.Logger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logger = logger
}

// Reset clears the warning state for a new config load cycle
func (r *DeprecationRegistry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.warnings = make(map[string]bool)
}

// Check examines the config for deprecated options and logs warnings once per cycle
// Returns the list of deprecations that are in use
func (r *DeprecationRegistry) Check(cfg *Config) []Deprecation {
	return r.CheckWithKeys(cfg, nil)
}

// CheckWithKeys examines the config for deprecated options using key presence when available.
// If usedKeys is provided and a deprecation has KeyName set, presence-based detection is used.
// Otherwise, falls back to value-based CheckFunc.
func (r *DeprecationRegistry) CheckWithKeys(cfg *Config, usedKeys map[string]bool) []Deprecation {
	r.mu.Lock()
	defer r.mu.Unlock()

	var found []Deprecation

	for _, dep := range Deprecations {
		isDeprecated := false

		// Prefer key-presence detection when available
		if dep.KeyName != "" && usedKeys != nil {
			isDeprecated = usedKeys[dep.KeyName]
		} else if dep.CheckFunc != nil {
			// Fall back to value-based detection
			isDeprecated = dep.CheckFunc(cfg)
		}

		if isDeprecated {
			found = append(found, dep)

			// Only warn if we haven't warned about this option in this cycle
			if !r.warnings[dep.Option] {
				r.warnings[dep.Option] = true
				r.logWarning(dep)
			}
		}
	}

	return found
}

// ForDoctor returns all deprecated options in use without logging warnings
// This is used by 'ofelia doctor' to report deprecations
func (r *DeprecationRegistry) ForDoctor(cfg *Config) []Deprecation {
	return r.ForDoctorWithKeys(cfg, nil)
}

// ForDoctorWithKeys returns all deprecated options in use using key presence when available.
// This is used by 'ofelia doctor' to report deprecations with better detection.
func (r *DeprecationRegistry) ForDoctorWithKeys(cfg *Config, usedKeys map[string]bool) []Deprecation {
	var found []Deprecation

	for _, dep := range Deprecations {
		isDeprecated := false

		// Prefer key-presence detection when available
		if dep.KeyName != "" && usedKeys != nil {
			isDeprecated = usedKeys[dep.KeyName]
		} else if dep.CheckFunc != nil {
			// Fall back to value-based detection
			isDeprecated = dep.CheckFunc(cfg)
		}

		if isDeprecated {
			found = append(found, dep)
		}
	}

	return found
}

// logWarning outputs a deprecation warning
func (r *DeprecationRegistry) logWarning(dep Deprecation) {
	// Always write to stderr for visibility
	fmt.Fprintf(os.Stderr, "DEPRECATION WARNING: '%s' is deprecated and will be removed in %s.\n",
		dep.Option, dep.RemovalVersion)
	fmt.Fprintf(os.Stderr, "  Replacement: %s\n", dep.Replacement)
	if dep.Message != "" {
		fmt.Fprintf(os.Stderr, "  %s\n", dep.Message)
	}
	fmt.Fprintln(os.Stderr)

	// Also log via logger if available
	if r.logger != nil {
		r.logger.Warningf("DEPRECATED: '%s' is deprecated and will be removed in %s. Use %s instead.",
			dep.Option, dep.RemovalVersion, dep.Replacement)
	}
}

// GetDeprecationRegistry returns the global deprecation registry
func GetDeprecationRegistry() *DeprecationRegistry {
	return deprecationRegistry
}

// CheckDeprecations is a convenience function to check for deprecated options
// Call this after loading/reloading config
func CheckDeprecations(cfg *Config) []Deprecation {
	return deprecationRegistry.Check(cfg)
}

// CheckDeprecationsWithKeys is a convenience function to check for deprecated options with key presence
// Call this after loading/reloading config when you have key metadata available
func CheckDeprecationsWithKeys(cfg *Config, usedKeys map[string]bool) []Deprecation {
	return deprecationRegistry.CheckWithKeys(cfg, usedKeys)
}

// ResetDeprecationWarnings resets the warning state for a new config load cycle
func ResetDeprecationWarnings() {
	deprecationRegistry.Reset()
}

// ApplyDeprecationMigrations applies all BC migrations for deprecated options
// This should be called during config loading to ensure deprecated options still work
func ApplyDeprecationMigrations(cfg *Config) {
	ApplyDeprecationMigrationsWithKeys(cfg, nil)
}

// ApplyDeprecationMigrationsWithKeys applies all BC migrations using key presence when available
// This should be called during config loading to ensure deprecated options still work
func ApplyDeprecationMigrationsWithKeys(cfg *Config, usedKeys map[string]bool) {
	for _, dep := range Deprecations {
		if dep.MigrateFunc == nil {
			continue
		}

		shouldMigrate := false

		// Prefer key-presence detection when available
		if dep.KeyName != "" && usedKeys != nil {
			shouldMigrate = usedKeys[dep.KeyName]
		} else if dep.CheckFunc != nil {
			// Fall back to value-based detection
			shouldMigrate = dep.CheckFunc(cfg)
		}

		if shouldMigrate {
			dep.MigrateFunc(cfg)
		}
	}
}
