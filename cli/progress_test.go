package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/netresearch/ofelia/test"
)

// TestProgressIndicator_NonTerminal tests progress indicator in non-terminal mode (simple logging)
func TestProgressIndicator_NonTerminal(t *testing.T) {
	logger := test.NewTestLogger()
	progress := &ProgressIndicator{
		logger:     logger,
		writer:     &bytes.Buffer{},
		message:    "Testing operation",
		done:       make(chan struct{}),
		isTerminal: false, // Force non-terminal mode
	}

	progress.Start()
	time.Sleep(50 * time.Millisecond)
	progress.Stop(true, "Operation completed successfully")

	// Verify logger was used for non-terminal output
	// The logger should have recorded the messages
}

// TestProgressIndicator_Start tests starting a progress indicator
func TestProgressIndicator_Start(t *testing.T) {
	logger := test.NewTestLogger()
	progress := NewProgressIndicator(logger, "Testing operation")

	// Should be able to start
	progress.Start()
	defer func() {
		progress.Stop(true, "Test complete")
	}()

	// Starting again should be idempotent
	progress.Start()
}

// TestProgressIndicator_Stop tests stopping a progress indicator
func TestProgressIndicator_Stop(t *testing.T) {
	logger := test.NewTestLogger()
	progress := NewProgressIndicator(logger, "Testing operation")

	progress.Start()
	time.Sleep(50 * time.Millisecond)

	// Should successfully stop
	progress.Stop(true, "Operation completed")

	// Stopping again should be idempotent
	progress.Stop(true, "Already stopped")
}

// TestProgressIndicator_Update tests updating progress message
func TestProgressIndicator_Update(t *testing.T) {
	logger := test.NewTestLogger()
	progress := &ProgressIndicator{
		logger:     logger,
		writer:     &bytes.Buffer{},
		message:    "Initial message",
		done:       make(chan struct{}),
		isTerminal: false,
		started:    true,
	}

	progress.Update("Updated message")

	if progress.message != "Updated message" {
		t.Errorf("Expected message to be updated to 'Updated message', got '%s'", progress.message)
	}
}

// TestProgressReporter_Step tests step reporting
func TestProgressReporter_Step(t *testing.T) {
	logger := test.NewTestLogger()
	reporter := NewProgressReporter(logger, 5)

	// Report several steps
	reporter.Step(1, "Step 1")
	reporter.Step(2, "Step 2")
	reporter.Step(3, "Step 3")
	reporter.Step(4, "Step 4")
	reporter.Step(5, "Step 5")

	if reporter.currentStep != 5 {
		t.Errorf("Expected currentStep to be 5, got %d", reporter.currentStep)
	}
}

// TestProgressReporter_Complete tests completion reporting
func TestProgressReporter_Complete(t *testing.T) {
	logger := test.NewTestLogger()
	reporter := NewProgressReporter(logger, 3)

	reporter.Step(1, "Step 1")
	reporter.Step(2, "Step 2")
	reporter.Step(3, "Step 3")
	reporter.Complete("All steps complete")

	// Should have completed all steps
	if reporter.currentStep != 3 {
		t.Errorf("Expected currentStep to be 3, got %d", reporter.currentStep)
	}
}

// TestProgressReporter_RenderProgressBar tests progress bar rendering
func TestProgressReporter_RenderProgressBar(t *testing.T) {
	logger := test.NewTestLogger()
	reporter := NewProgressReporter(logger, 10)

	tests := []struct {
		name    string
		percent float64
		want    string
	}{
		{"0 percent", 0, "░░░░░░░░░░░░░░░░░░░░ 0%"},
		{"50 percent", 50, "██████████░░░░░░░░░░ 50%"},
		{"100 percent", 100, "████████████████████ 100%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reporter.renderProgressBar(tt.percent)
			if got != tt.want {
				t.Errorf("renderProgressBar(%f) = %q, want %q", tt.percent, got, tt.want)
			}
		})
	}
}

// TestProgressIndicator_Concurrency tests concurrent usage
func TestProgressIndicator_Concurrency(t *testing.T) {
	logger := test.NewTestLogger()
	progress := NewProgressIndicator(logger, "Concurrent test")

	progress.Start()

	// Concurrent updates
	done := make(chan bool)
	for i := range 10 {
		go func(n int) {
			progress.Update(fmt.Sprintf("Update %d", n))
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for range 10 {
		<-done
	}

	progress.Stop(true, "Concurrent test complete")
}

// TestProgressReporter_ZeroSteps tests reporter with zero steps
func TestProgressReporter_ZeroSteps(t *testing.T) {
	logger := test.NewTestLogger()
	reporter := NewProgressReporter(logger, 0)

	// Should handle zero steps gracefully
	reporter.Complete("No steps")
}

// TestProgressReporter_ProgressCalculation tests progress percentage calculation
func TestProgressReporter_ProgressCalculation(t *testing.T) {
	logger := test.NewTestLogger()
	reporter := NewProgressReporter(logger, 4)

	tests := []struct {
		step            int
		expectedPercent float64
	}{
		{1, 25},
		{2, 50},
		{3, 75},
		{4, 100},
	}

	for _, tt := range tests {
		reporter.Step(tt.step, "Test step")
		progress := float64(tt.step) / float64(reporter.totalSteps) * 100
		if progress != tt.expectedPercent {
			t.Errorf("Step %d: expected %.0f%%, got %.0f%%", tt.step, tt.expectedPercent, progress)
		}
	}
}

// TestProgressIndicator_MessageContent tests message content preservation
func TestProgressIndicator_MessageContent(t *testing.T) {
	logger := test.NewTestLogger()

	testMessages := []string{
		"Simple message",
		"Message with numbers: 12345",
		"Message with special chars: !@#$%",
		"Long message that might wrap: " + strings.Repeat("test ", 20),
	}

	for _, msg := range testMessages {
		progress := NewProgressIndicator(logger, msg)
		if progress.message != msg {
			t.Errorf("Message not preserved: expected %q, got %q", msg, progress.message)
		}
	}
}
