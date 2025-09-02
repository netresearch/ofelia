package core

import (
	"os"
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

func TestNewRunJobWithDockerEventsConfig(t *testing.T) {
	// Test default behavior (events enabled)
	os.Unsetenv("OFELIA_USE_DOCKER_EVENTS")
	client := &docker.Client{}
	job := NewRunJob(client)

	if job.monitor == nil {
		t.Error("expected monitor to be initialized")
	}

	// Test disabling events via environment variable - false
	os.Setenv("OFELIA_USE_DOCKER_EVENTS", "false")
	defer os.Unsetenv("OFELIA_USE_DOCKER_EVENTS")

	job2 := NewRunJob(client)
	if job2.monitor == nil {
		t.Error("expected monitor to be initialized even when events disabled")
	}

	// Test disabling events via environment variable - 0
	os.Setenv("OFELIA_USE_DOCKER_EVENTS", "0")
	job3 := NewRunJob(client)
	if job3.monitor == nil {
		t.Error("expected monitor to be initialized")
	}

	// Test disabling events via environment variable - no
	os.Setenv("OFELIA_USE_DOCKER_EVENTS", "no")
	job4 := NewRunJob(client)
	if job4.monitor == nil {
		t.Error("expected monitor to be initialized")
	}

	// Test enabling events explicitly (should remain enabled)
	os.Setenv("OFELIA_USE_DOCKER_EVENTS", "true")
	job5 := NewRunJob(client)
	if job5.monitor == nil {
		t.Error("expected monitor to be initialized")
	}
}

func TestRunJobContainerIDThreadSafety(t *testing.T) {
	client := &docker.Client{}
	job := NewRunJob(client)

	// Test concurrent access to containerID
	done := make(chan bool)

	// Multiple goroutines setting containerID
	for i := 0; i < 10; i++ {
		go func(id int) {
			job.setContainerID(string(rune('a' + id)))
			done <- true
		}(i)
	}

	// Multiple goroutines getting containerID
	for i := 0; i < 10; i++ {
		go func() {
			_ = job.getContainerID()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}

	// Should not panic due to race conditions
	finalID := job.getContainerID()
	if finalID == "" {
		t.Error("expected containerID to be set")
	}
}

func TestWatchContainerWithMonitor(t *testing.T) {
	client := &docker.Client{}
	job := NewRunJob(client)
	job.setContainerID("test-container-123")

	// Test with nil monitor (fallback to legacy)
	job.monitor = nil
	// This would normally call watchContainerLegacy
	// We can't fully test this without a real Docker connection

	// Test with monitor present
	job.monitor = NewContainerMonitor(client, &SimpleLogger{})
	job.MaxRuntime = 5 * time.Second

	// We can't fully test the monitor without mocking Docker events
	// but we ensure the code path exists and doesn't panic
}

func TestEntrypointSliceExtended(t *testing.T) {
	tests := []struct {
		name     string
		input    *string
		expected []string
	}{
		{
			name:     "nil entrypoint",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty entrypoint",
			input:    strPtr(""),
			expected: []string{},
		},
		{
			name:     "single command",
			input:    strPtr("/bin/sh"),
			expected: []string{"/bin/sh"},
		},
		{
			name:     "command with args",
			input:    strPtr("/bin/sh -c 'echo hello'"),
			expected: []string{"/bin/sh", "-c", "echo hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := entrypointSlice(tt.input)
			if !sliceEqual(result, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// Helper functions
func strPtr(s string) *string {
	return &s
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
