package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// TestDaemonCommand_Execute_BootError tests Execute with boot failure
func TestDaemonCommand_Execute_BootError(t *testing.T) {
	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	logger := test.NewTestLogger()
	cmd := &DaemonCommand{
		ConfigFile: "",
		Logger:     logger,
	}

	err := cmd.Execute(nil)

	if err == nil {
		t.Error("Expected error but got nil")
	}
}

// TestDaemonCommand_Config tests the Config getter method
func TestDaemonCommand_Config(t *testing.T) {
	logger := test.NewTestLogger()

	// Create a daemon with a config
	cmd := &DaemonCommand{
		Logger: logger,
	}

	// Initially nil
	if cmd.Config() != nil {
		t.Error("Expected nil config before boot")
	}

	// Set up mock to return a valid config
	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		mockProvider := &mockDockerProviderForHandler{}
		return orig(ctx, notifier, logger, cfg, mockProvider)
	}

	// Boot the daemon
	err := cmd.boot()
	if err != nil {
		t.Fatalf("boot failed: %v", err)
	}

	// Now config should be set
	cfg := cmd.Config()
	if cfg == nil {
		t.Error("Expected non-nil config after boot")
	}

	// Verify it's the same config instance
	if cmd.config != cfg {
		t.Error("Config() should return the internal config field")
	}
}
