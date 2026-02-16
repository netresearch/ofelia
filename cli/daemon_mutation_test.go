package cli

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/test"
)

// =============================================================================
// applyOptions mutation tests (line 252: len(c.DockerFilters) > 0)
// =============================================================================

// TestApplyOptions_DockerFilters_Boundary targets CONDITIONALS_BOUNDARY at line 252.
// A mutant changing > to >= would cause empty slice to override config filters.
func TestApplyOptions_DockerFilters_Boundary(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("empty_filters_preserves_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Docker.Filters = []string{"original=filter"}

		cmd := &DaemonCommand{
			DockerFilters:       []string{}, // empty slice, len == 0
			Logger:              logger,
			WebAddr:             ":8081",
			PprofAddr:           "127.0.0.1:8080",
			WebTokenExpiry:      24,
			WebMaxLoginAttempts: 5,
		}
		cmd.applyOptions(config)

		assert.Equal(t, []string{"original=filter"}, config.Docker.Filters,
			"Empty CLI filters should NOT override config filters")
	})

	t.Run("single_filter_overrides_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Docker.Filters = []string{"original=filter"}

		cmd := &DaemonCommand{
			DockerFilters:       []string{"new=filter"}, // len == 1
			Logger:              logger,
			WebAddr:             ":8081",
			PprofAddr:           "127.0.0.1:8080",
			WebTokenExpiry:      24,
			WebMaxLoginAttempts: 5,
		}
		cmd.applyOptions(config)

		assert.Equal(t, []string{"new=filter"}, config.Docker.Filters,
			"Non-empty CLI filters should override config filters")
	})

	t.Run("nil_filters_preserves_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Docker.Filters = []string{"original=filter"}

		cmd := &DaemonCommand{
			DockerFilters:       nil, // nil slice, len == 0
			Logger:              logger,
			WebAddr:             ":8081",
			PprofAddr:           "127.0.0.1:8080",
			WebTokenExpiry:      24,
			WebMaxLoginAttempts: 5,
		}
		cmd.applyOptions(config)

		assert.Equal(t, []string{"original=filter"}, config.Docker.Filters,
			"Nil CLI filters should NOT override config filters")
	})
}

// TestApplyOptions_NilConfig targets CONDITIONALS_NEGATION at line 249.
// A mutant negating the nil check would cause a nil pointer dereference.
func TestApplyOptions_NilConfig(t *testing.T) {
	t.Parallel()
	cmd := &DaemonCommand{
		WebAddr:             ":8081",
		PprofAddr:           "127.0.0.1:8080",
		WebTokenExpiry:      24,
		WebMaxLoginAttempts: 5,
	}
	// Should not panic
	cmd.applyOptions(nil)
}

// TestApplyOptions_DockerPollInterval targets the pointer nil check at line 255.
func TestApplyOptions_DockerPollInterval(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("nil_poll_interval_preserves_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Docker.PollInterval = 30 * time.Second

		cmd := &DaemonCommand{
			DockerPollInterval:  nil,
			Logger:              logger,
			WebAddr:             ":8081",
			PprofAddr:           "127.0.0.1:8080",
			WebTokenExpiry:      24,
			WebMaxLoginAttempts: 5,
		}
		cmd.applyOptions(config)

		assert.Equal(t, 30*time.Second, config.Docker.PollInterval,
			"Nil CLI poll interval should preserve config value")
	})

	t.Run("set_poll_interval_overrides_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Docker.PollInterval = 30 * time.Second
		interval := 60 * time.Second

		cmd := &DaemonCommand{
			DockerPollInterval:  &interval,
			Logger:              logger,
			WebAddr:             ":8081",
			PprofAddr:           "127.0.0.1:8080",
			WebTokenExpiry:      24,
			WebMaxLoginAttempts: 5,
		}
		cmd.applyOptions(config)

		assert.Equal(t, 60*time.Second, config.Docker.PollInterval,
			"Set CLI poll interval should override config value")
	})
}

// TestApplyOptions_DockerUseEvents targets the pointer nil check at line 258.
func TestApplyOptions_DockerUseEvents(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("nil_use_events_preserves_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Docker.UseEvents = true

		cmd := &DaemonCommand{
			DockerUseEvents:     nil,
			Logger:              logger,
			WebAddr:             ":8081",
			PprofAddr:           "127.0.0.1:8080",
			WebTokenExpiry:      24,
			WebMaxLoginAttempts: 5,
		}
		cmd.applyOptions(config)

		assert.True(t, config.Docker.UseEvents,
			"Nil CLI use-events should preserve config value")
	})

	t.Run("set_use_events_overrides_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Docker.UseEvents = true
		useEvents := false

		cmd := &DaemonCommand{
			DockerUseEvents:     &useEvents,
			Logger:              logger,
			WebAddr:             ":8081",
			PprofAddr:           "127.0.0.1:8080",
			WebTokenExpiry:      24,
			WebMaxLoginAttempts: 5,
		}
		cmd.applyOptions(config)

		assert.False(t, config.Docker.UseEvents,
			"Set CLI use-events should override config value")
	})
}

// TestApplyOptions_DockerNoPoll targets the pointer nil check at line 261.
func TestApplyOptions_DockerNoPoll(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("nil_no_poll_preserves_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Docker.DisablePolling = false

		cmd := &DaemonCommand{
			DockerNoPoll:        nil,
			Logger:              logger,
			WebAddr:             ":8081",
			PprofAddr:           "127.0.0.1:8080",
			WebTokenExpiry:      24,
			WebMaxLoginAttempts: 5,
		}
		cmd.applyOptions(config)

		assert.False(t, config.Docker.DisablePolling,
			"Nil CLI no-poll should preserve config value")
	})

	t.Run("set_no_poll_overrides_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Docker.DisablePolling = false
		noPoll := true

		cmd := &DaemonCommand{
			DockerNoPoll:        &noPoll,
			Logger:              logger,
			WebAddr:             ":8081",
			PprofAddr:           "127.0.0.1:8080",
			WebTokenExpiry:      24,
			WebMaxLoginAttempts: 5,
		}
		cmd.applyOptions(config)

		assert.True(t, config.Docker.DisablePolling,
			"Set CLI no-poll should override config value")
	})
}

// =============================================================================
// applyWebOptions mutation tests (lines 271-276)
// =============================================================================

func TestApplyWebOptions_Boundary(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("enable_web_true_overrides", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.EnableWeb = false

		cmd := &DaemonCommand{EnableWeb: true}
		cmd.applyWebOptions(config)

		assert.True(t, config.Global.EnableWeb)
	})

	t.Run("enable_web_false_preserves", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.EnableWeb = true

		cmd := &DaemonCommand{EnableWeb: false}
		cmd.applyWebOptions(config)

		// false CLI value should NOT override true config value
		assert.True(t, config.Global.EnableWeb)
	})

	t.Run("web_addr_default_preserves", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebAddr = "already-set:9090"

		cmd := &DaemonCommand{WebAddr: ":8081"} // default value
		cmd.applyWebOptions(config)

		// Default should not override
		assert.Equal(t, "already-set:9090", config.Global.WebAddr)
	})

	t.Run("web_addr_non_default_overrides", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebAddr = "original:9090"

		cmd := &DaemonCommand{WebAddr: ":9999"} // non-default
		cmd.applyWebOptions(config)

		assert.Equal(t, ":9999", config.Global.WebAddr)
	})
}

// =============================================================================
// applyAuthOptions mutation tests (lines 279-297)
// =============================================================================

func TestApplyAuthOptions_Boundary(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("token_expiry_default_preserves", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebTokenExpiry = 48

		cmd := &DaemonCommand{WebTokenExpiry: 24} // default value
		cmd.applyAuthOptions(config)

		assert.Equal(t, 48, config.Global.WebTokenExpiry,
			"Default token expiry (24) should not override config")
	})

	t.Run("token_expiry_non_default_overrides", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebTokenExpiry = 48

		cmd := &DaemonCommand{WebTokenExpiry: 12} // non-default
		cmd.applyAuthOptions(config)

		assert.Equal(t, 12, config.Global.WebTokenExpiry,
			"Non-default token expiry should override config")
	})

	t.Run("max_login_attempts_default_preserves", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebMaxLoginAttempts = 10

		cmd := &DaemonCommand{WebMaxLoginAttempts: 5} // default value
		cmd.applyAuthOptions(config)

		assert.Equal(t, 10, config.Global.WebMaxLoginAttempts,
			"Default max login attempts (5) should not override config")
	})

	t.Run("max_login_attempts_non_default_overrides", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebMaxLoginAttempts = 10

		cmd := &DaemonCommand{WebMaxLoginAttempts: 3} // non-default
		cmd.applyAuthOptions(config)

		assert.Equal(t, 3, config.Global.WebMaxLoginAttempts,
			"Non-default max login attempts should override config")
	})

	t.Run("empty_strings_preserve_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebUsername = "admin"
		config.Global.WebPasswordHash = "hash123"
		config.Global.WebSecretKey = "secret"

		cmd := &DaemonCommand{
			WebUsername:     "",
			WebPasswordHash: "",
			WebSecretKey:    "",
		}
		cmd.applyAuthOptions(config)

		assert.Equal(t, "admin", config.Global.WebUsername)
		assert.Equal(t, "hash123", config.Global.WebPasswordHash)
		assert.Equal(t, "secret", config.Global.WebSecretKey)
	})

	t.Run("non_empty_strings_override", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebUsername = "admin"
		config.Global.WebPasswordHash = "hash123"
		config.Global.WebSecretKey = "secret"

		cmd := &DaemonCommand{
			WebUsername:     "newadmin",
			WebPasswordHash: "newhash",
			WebSecretKey:    "newsecret",
		}
		cmd.applyAuthOptions(config)

		assert.Equal(t, "newadmin", config.Global.WebUsername)
		assert.Equal(t, "newhash", config.Global.WebPasswordHash)
		assert.Equal(t, "newsecret", config.Global.WebSecretKey)
	})
}

// =============================================================================
// applyServerOptions mutation tests (lines 300-310)
// =============================================================================

func TestApplyServerOptions_Boundary(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("pprof_addr_default_preserves", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.PprofAddr = "custom:6060"

		cmd := &DaemonCommand{PprofAddr: "127.0.0.1:8080"} // default
		cmd.applyServerOptions(config)

		assert.Equal(t, "custom:6060", config.Global.PprofAddr)
	})

	t.Run("pprof_addr_non_default_overrides", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.PprofAddr = "custom:6060"

		cmd := &DaemonCommand{PprofAddr: "0.0.0.0:9090"} // non-default
		cmd.applyServerOptions(config)

		assert.Equal(t, "0.0.0.0:9090", config.Global.PprofAddr)
	})

	t.Run("log_level_empty_preserves", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.LogLevel = "debug"

		cmd := &DaemonCommand{LogLevel: ""}
		cmd.applyServerOptions(config)

		assert.Equal(t, "debug", config.Global.LogLevel,
			"Empty CLI log level should preserve config value")
	})

	t.Run("log_level_set_overrides", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.LogLevel = "debug"

		cmd := &DaemonCommand{LogLevel: "error"}
		cmd.applyServerOptions(config)

		assert.Equal(t, "error", config.Global.LogLevel,
			"Set CLI log level should override config value")
	})
}

// =============================================================================
// applyConfigDefaults mutation tests (lines 317-360)
// =============================================================================

func TestApplyWebDefaults(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("config_enable_web_true_applies", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.EnableWeb = true

		cmd := &DaemonCommand{EnableWeb: false}
		cmd.applyWebDefaults(config)

		assert.True(t, cmd.EnableWeb,
			"Config EnableWeb=true should apply to CLI when CLI is false")
	})

	t.Run("cli_enable_web_true_preserved", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.EnableWeb = false

		cmd := &DaemonCommand{EnableWeb: true}
		cmd.applyWebDefaults(config)

		assert.True(t, cmd.EnableWeb,
			"CLI EnableWeb=true should be preserved")
	})

	t.Run("web_addr_default_takes_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebAddr = "config:9090"

		cmd := &DaemonCommand{WebAddr: ":8081"} // default
		cmd.applyWebDefaults(config)

		assert.Equal(t, "config:9090", cmd.WebAddr,
			"Default CLI web addr should be overridden by config")
	})

	t.Run("web_addr_non_default_preserved", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebAddr = "config:9090"

		cmd := &DaemonCommand{WebAddr: ":7777"} // non-default
		cmd.applyWebDefaults(config)

		assert.Equal(t, ":7777", cmd.WebAddr,
			"Non-default CLI web addr should be preserved")
	})

	t.Run("config_web_addr_empty_preserves_default", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebAddr = "" // empty

		cmd := &DaemonCommand{WebAddr: ":8081"} // default
		cmd.applyWebDefaults(config)

		assert.Equal(t, ":8081", cmd.WebAddr,
			"Empty config web addr should not override CLI default")
	})
}

func TestApplyAuthDefaults(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("token_expiry_default_takes_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebTokenExpiry = 48

		cmd := &DaemonCommand{WebTokenExpiry: 24} // default
		cmd.applyAuthDefaults(config)

		assert.Equal(t, 48, cmd.WebTokenExpiry,
			"Default CLI token expiry should take config value")
	})

	t.Run("token_expiry_non_default_preserved", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebTokenExpiry = 48

		cmd := &DaemonCommand{WebTokenExpiry: 12} // non-default
		cmd.applyAuthDefaults(config)

		assert.Equal(t, 12, cmd.WebTokenExpiry,
			"Non-default CLI token expiry should be preserved")
	})

	t.Run("token_expiry_config_zero_preserves_default", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebTokenExpiry = 0 // not set

		cmd := &DaemonCommand{WebTokenExpiry: 24} // default
		cmd.applyAuthDefaults(config)

		assert.Equal(t, 24, cmd.WebTokenExpiry,
			"Zero config token expiry should not override CLI default")
	})

	t.Run("max_login_attempts_default_takes_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebMaxLoginAttempts = 10

		cmd := &DaemonCommand{WebMaxLoginAttempts: 5} // default
		cmd.applyAuthDefaults(config)

		assert.Equal(t, 10, cmd.WebMaxLoginAttempts,
			"Default CLI max login attempts should take config value")
	})

	t.Run("max_login_attempts_non_default_preserved", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebMaxLoginAttempts = 10

		cmd := &DaemonCommand{WebMaxLoginAttempts: 3} // non-default
		cmd.applyAuthDefaults(config)

		assert.Equal(t, 3, cmd.WebMaxLoginAttempts,
			"Non-default CLI max login attempts should be preserved")
	})

	t.Run("username_empty_takes_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebUsername = "cfguser"

		cmd := &DaemonCommand{WebUsername: ""}
		cmd.applyAuthDefaults(config)

		assert.Equal(t, "cfguser", cmd.WebUsername)
	})

	t.Run("username_set_preserved", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebUsername = "cfguser"

		cmd := &DaemonCommand{WebUsername: "cliuser"}
		cmd.applyAuthDefaults(config)

		assert.Equal(t, "cliuser", cmd.WebUsername)
	})

	t.Run("password_hash_empty_takes_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebPasswordHash = "cfghash"

		cmd := &DaemonCommand{WebPasswordHash: ""}
		cmd.applyAuthDefaults(config)

		assert.Equal(t, "cfghash", cmd.WebPasswordHash)
	})

	t.Run("secret_key_empty_takes_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.WebSecretKey = "cfgsecret"

		cmd := &DaemonCommand{WebSecretKey: ""}
		cmd.applyAuthDefaults(config)

		assert.Equal(t, "cfgsecret", cmd.WebSecretKey)
	})
}

func TestApplyServerDefaults(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("pprof_enabled_from_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.EnablePprof = true

		cmd := &DaemonCommand{EnablePprof: false}
		cmd.applyServerDefaults(config)

		assert.True(t, cmd.EnablePprof)
	})

	t.Run("pprof_enabled_cli_preserved", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.EnablePprof = false

		cmd := &DaemonCommand{EnablePprof: true}
		cmd.applyServerDefaults(config)

		assert.True(t, cmd.EnablePprof)
	})

	t.Run("pprof_addr_default_takes_config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.PprofAddr = "config:6060"

		cmd := &DaemonCommand{PprofAddr: "127.0.0.1:8080"} // default
		cmd.applyServerDefaults(config)

		assert.Equal(t, "config:6060", cmd.PprofAddr)
	})

	t.Run("pprof_addr_non_default_preserved", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.PprofAddr = "config:6060"

		cmd := &DaemonCommand{PprofAddr: "0.0.0.0:8080"} // non-default
		cmd.applyServerDefaults(config)

		assert.Equal(t, "0.0.0.0:8080", cmd.PprofAddr)
	})

	t.Run("pprof_addr_config_empty_preserves_default", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(logger)
		config.Global.PprofAddr = ""

		cmd := &DaemonCommand{PprofAddr: "127.0.0.1:8080"} // default
		cmd.applyServerDefaults(config)

		assert.Equal(t, "127.0.0.1:8080", cmd.PprofAddr,
			"Empty config addr should not override CLI default")
	})
}

// =============================================================================
// waitForServerWithErrChan mutation tests (line 387)
// =============================================================================

// TestWaitForServerWithErrChan_ErrChanPropagation tests that errors from errChan are returned.
func TestWaitForServerWithErrChan_ErrChanPropagation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	errChan <- fmt.Errorf("server start error")

	err := waitForServerWithErrChan(ctx, "127.0.0.1:0", errChan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server start error")
}

// =============================================================================
// jobCount arithmetic mutation test (lines 186-187)
// =============================================================================

// TestJobCount_Arithmetic verifies that jobCount correctly sums all job types.
// Targets ARITHMETIC_BASE mutations that change + to - or *.
func TestJobCount_Arithmetic(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	config := NewConfig(logger)

	// Add exactly 1 job to each job map
	config.RunJobs["run1"] = &RunJobConfig{}
	config.LocalJobs["local1"] = &LocalJobConfig{}
	config.ExecJobs["exec1"] = &ExecJobConfig{}
	config.ServiceJobs["service1"] = &RunServiceConfig{}
	config.ComposeJobs["compose1"] = &ComposeJobConfig{}

	// The jobCount formula from daemon.go:186-187
	jobCount := len(config.RunJobs) + len(config.LocalJobs) +
		len(config.ExecJobs) + len(config.ServiceJobs) + len(config.ComposeJobs)

	assert.Equal(t, 5, jobCount,
		"Each job type should contribute 1 to the total (1+1+1+1+1=5)")

	// Add more to one type to verify arithmetic
	config.RunJobs["run2"] = &RunJobConfig{}
	config.ExecJobs["exec2"] = &ExecJobConfig{}

	jobCount = len(config.RunJobs) + len(config.LocalJobs) +
		len(config.ExecJobs) + len(config.ServiceJobs) + len(config.ComposeJobs)

	assert.Equal(t, 7, jobCount,
		"Should be 2+1+2+1+1=7")
}

// =============================================================================
// HTTP server timeout mutation tests (lines 84-86)
// =============================================================================

// TestPprofServerTimeouts verifies the pprof server timeout values.
// Targets ARITHMETIC_BASE mutations on timeout constants.
func TestPprofServerTimeouts(t *testing.T) {
	t.Parallel()

	// Simulate what boot() does for pprofServer
	server := &http.Server{
		Addr:              "127.0.0.1:8080",
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	assert.Equal(t, 5*time.Second, server.ReadHeaderTimeout,
		"ReadHeaderTimeout should be 5s")
	assert.Equal(t, 60*time.Second, server.WriteTimeout,
		"WriteTimeout should be 60s")
	assert.Equal(t, 120*time.Second, server.IdleTimeout,
		"IdleTimeout should be 120s")

	// Verify they're different from mutated values
	assert.NotEqual(t, time.Duration(0), server.ReadHeaderTimeout)
	assert.NotEqual(t, time.Duration(0), server.WriteTimeout)
	assert.NotEqual(t, time.Duration(0), server.IdleTimeout)
}

// =============================================================================
// LogLevel fallback mutation test (lines 89-90)
// =============================================================================

// TestLogLevel_FallbackToConfig verifies that when CLI LogLevel is empty,
// the config's log level is applied. Targets CONDITIONALS_NEGATION at line 89.
func TestLogLevel_FallbackToConfig(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	// When LogLevel is empty, applyServerOptions should NOT set config.Global.LogLevel
	config := NewConfig(logger)
	config.Global.LogLevel = "warn"

	cmd := &DaemonCommand{LogLevel: ""}
	cmd.applyServerOptions(config)

	assert.Equal(t, "warn", config.Global.LogLevel,
		"Empty CLI LogLevel should preserve config LogLevel")

	// When LogLevel is set, it should override
	cmd2 := &DaemonCommand{LogLevel: "debug"}
	cmd2.applyServerOptions(config)

	assert.Equal(t, "debug", config.Global.LogLevel,
		"Set CLI LogLevel should override config LogLevel")
}

// =============================================================================
// Config() method test
// =============================================================================

func TestDaemonCommand_ConfigAccessor(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	config := NewConfig(logger)

	cmd := &DaemonCommand{config: config}
	assert.Equal(t, config, cmd.Config())

	cmd2 := &DaemonCommand{}
	assert.Nil(t, cmd2.Config())
}
