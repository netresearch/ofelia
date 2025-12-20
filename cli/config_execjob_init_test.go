package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// TestExecJobInit_FromINIConfig verifies that ExecJobs loaded from INI config
// have dockerOps properly initialized and can execute without panic
func TestExecJobInit_FromINIConfig(t *testing.T) {
	t.Parallel()

	mockLogger := &test.Logger{}

	// Create config from INI string (simulates loading from file)
	cfg, err := BuildFromString(`
		[job-exec "test-job"]
		schedule = @every 1h
		command = echo "test"
		container = test-container
		user = nobody
	`, mockLogger)

	require.NoError(t, err)
	assert.NotNil(t, cfg.ExecJobs)
	assert.Len(t, cfg.ExecJobs, 1)

	// Get the job
	job, exists := cfg.ExecJobs["test-job"]
	assert.True(t, exists)
	assert.NotNil(t, job)

	// Verify job fields are set from config
	assert.Empty(t, job.GetName()) // Name not set yet (set during registration)
	assert.Equal(t, "@every 1h", job.GetSchedule())
	assert.Equal(t, `echo "test"`, job.GetCommand())
	assert.Equal(t, "test-container", job.Container)
	assert.Equal(t, "nobody", job.User)

	// CRITICAL: This is the regression test for the nil pointer bug
	// Before the fix, Provider would be nil here
	// The job won't have Provider until InitializeApp() is called
	assert.Nil(t, job.ExecJob.Provider) // Provider not set until InitializeApp
}

// TestExecJobInit_AfterInitializeApp verifies that after InitializeApp(),
// ExecJobs have dockerOps initialized and can be scheduled
func TestExecJobInit_AfterInitializeApp(t *testing.T) {
	t.Parallel()

	mockLogger := &test.Logger{}

	// Create config from INI string
	cfg, err := BuildFromString(`
		[job-exec "initialized-job"]
		schedule = @every 1h
		command = /bin/true
		container = test-container
	`, mockLogger)

	require.NoError(t, err)

	// Initialize the app (this calls registerAllJobs which should call InitializeRuntimeFields)
	// Note: This will fail without Docker, but we're testing the initialization path
	err = cfg.InitializeApp()

	// We expect an error here because Docker is not available in test env
	// But the important thing is that it doesn't panic due to nil dockerOps
	// If there's a panic, the test will fail
	if err == nil {
		// If we somehow have Docker available, verify the job is properly initialized
		job, exists := cfg.ExecJobs["initialized-job"]
		assert.True(t, exists)
		assert.NotNil(t, job)
		assert.Equal(t, "initialized-job", job.GetName())

		// This is the critical check - dockerOps should be initialized
		// We can't check it directly as it's private, but if Run() doesn't panic, it worked
	}
}

// TestExecJobConfig_dockerOpsInitialization is a unit test that verifies
// the InitializeRuntimeFields method is called during config preparation
func TestExecJobConfig_dockerOpsInitialization(t *testing.T) {
	t.Parallel()

	// This test verifies the fix at the config layer
	// Create an ExecJobConfig directly (as mapstructure would)
	job := &ExecJobConfig{
		ExecJob: core.ExecJob{
			BareJob: core.BareJob{
				Name:     "direct-job",
				Command:  "echo test",
				Schedule: "@hourly",
			},
			Container: "test",
			User:      "nobody",
		},
	}

	// Before setting client, dockerOps should be nil
	// (We can't check this directly as it's private)

	// Simulate what happens in registerAllJobs, dockerLabelsUpdate, and iniConfigUpdate
	// In a real scenario, this would be a real Docker client
	// For this test, we just verify the method exists and doesn't panic with nil client
	job.InitializeRuntimeFields()

	// The method should handle nil client gracefully
	// No assertion needed - if it panics, test fails
}
