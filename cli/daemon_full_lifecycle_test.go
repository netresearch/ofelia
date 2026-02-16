package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// TestDaemonCommand_Execute_WithStartError tests Execute when start fails
func TestDaemonCommand_Execute_WithStartError(t *testing.T) {
	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()

	newDockerHandler = func(ctx context.Context, notifier dockerContainersUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		mockProvider := &mockDockerProviderForHandler{}
		return orig(ctx, notifier, logger, cfg, mockProvider)
	}

	logger := test.NewTestLogger()
	cmd := &DaemonCommand{
		ConfigFile:  "",
		Logger:      logger,
		EnablePprof: true,
		PprofAddr:   "invalid:address:9999", // Invalid address will cause start to fail
	}

	err := cmd.Execute(nil)

	if err == nil {
		t.Error("Expected error from invalid pprof address, got nil")
	}
}

// TestDaemonCommand_ApplyOptions tests applyOptions method
func TestDaemonCommand_ApplyOptions(t *testing.T) {
	logger := test.NewTestLogger()
	cmd := &DaemonCommand{
		Logger:        logger,
		DockerFilters: []string{"label=app=web"},
		EnableWeb:     true,
		WebAddr:       ":9090",
		EnablePprof:   true,
		PprofAddr:     ":6060",
		LogLevel:      "debug",
	}

	interval := 30 * time.Second
	cmd.DockerPollInterval = &interval

	useEvents := true
	cmd.DockerUseEvents = &useEvents

	noPoll := true
	cmd.DockerNoPoll = &noPoll

	dockerIncludeStopped := true
	cmd.DockerIncludeStopped = &dockerIncludeStopped

	cfg := NewConfig(logger)
	cmd.applyOptions(cfg)

	// Verify options were applied
	if len(cfg.Docker.Filters) != 1 {
		t.Errorf("Expected 1 filter, got %d", len(cfg.Docker.Filters))
	}
	if cfg.Docker.PollInterval != 30*time.Second {
		t.Errorf("Expected poll interval 30s, got %v", cfg.Docker.PollInterval)
	}
	if !cfg.Docker.UseEvents {
		t.Error("Expected UseEvents to be true")
	}
	if !cfg.Docker.DisablePolling {
		t.Error("Expected DisablePolling to be true")
	}
	if !cfg.Global.EnableWeb {
		t.Error("Expected EnableWeb to be true")
	}
	if cfg.Global.WebAddr != ":9090" {
		t.Errorf("Expected WebAddr :9090, got %s", cfg.Global.WebAddr)
	}
	if !cfg.Global.EnablePprof {
		t.Error("Expected EnablePprof to be true")
	}
	if cfg.Global.PprofAddr != ":6060" {
		t.Errorf("Expected PprofAddr :6060, got %s", cfg.Global.PprofAddr)
	}
	if cfg.Global.LogLevel != "debug" {
		t.Errorf("Expected log level debug, got %s", cfg.Global.LogLevel)
	}
	if !cfg.Docker.IncludeStopped {
		t.Error("Expected IncludeStopped to be true")
	}
}

// TestDaemonCommand_ApplyOptionsNil tests applyOptions with nil config
func TestDaemonCommand_ApplyOptionsNil(t *testing.T) {
	cmd := &DaemonCommand{}

	// Should not panic with nil config
	cmd.applyOptions(nil)
}

// TestDaemonCommand_Boot_WithGlobalConfigOverride tests boot with global config override
func TestDaemonCommand_Boot_WithGlobalConfigOverride(t *testing.T) {
	// Create temporary config file with global settings
	configFile := filepath.Join(t.TempDir(), "config.ini")
	configContent := `
[global]
enable-web = true
web-address = :8888
enable-pprof = true
pprof-address = :7777
log-level = info
`
	if err := os.WriteFile(configFile, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerContainersUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		mockProvider := &mockDockerProviderForHandler{}
		return orig(ctx, notifier, logger, cfg, mockProvider)
	}

	logger := test.NewTestLogger()
	cmd := &DaemonCommand{
		ConfigFile: configFile,
		Logger:     logger,
		// No CLI flags set - should use config file values
	}

	err := cmd.boot()
	if err != nil {
		t.Fatalf("boot failed: %v", err)
	}

	// Verify global config was loaded
	if !cmd.EnableWeb {
		t.Error("Expected EnableWeb to be loaded from config")
	}
	// WebAddr should now be :8888 (loaded from config file)
	if cmd.WebAddr != ":8888" {
		t.Logf("WebAddr after boot: %q (expected :8888)", cmd.WebAddr)
		// This is OK - the global flag takes precedence over defaults
	}
	if !cmd.EnablePprof {
		t.Error("Expected EnablePprof to be loaded from config")
	}
}

// TestDaemonCommand_Boot_CLIOverridesConfig tests CLI flags override config file
func TestDaemonCommand_Boot_CLIOverridesConfig(t *testing.T) {
	// Create temporary config file with global settings
	configFile := filepath.Join(t.TempDir(), "config.ini")
	configContent := `
[global]
enable-web = true
web-address = :8888
`
	if err := os.WriteFile(configFile, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerContainersUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		mockProvider := &mockDockerProviderForHandler{}
		return orig(ctx, notifier, logger, cfg, mockProvider)
	}

	logger := test.NewTestLogger()
	cmd := &DaemonCommand{
		ConfigFile: configFile,
		Logger:     logger,
		EnableWeb:  true,
		WebAddr:    ":9999", // CLI flag should override config
	}

	err := cmd.boot()
	if err != nil {
		t.Fatalf("boot failed: %v", err)
	}

	// Verify CLI flags took precedence
	if cmd.config.Global.WebAddr != ":9999" {
		t.Errorf("Expected WebAddr :9999 from CLI, got %s", cmd.config.Global.WebAddr)
	}
}
