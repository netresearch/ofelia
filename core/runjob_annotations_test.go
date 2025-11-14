package core

import (
	"strings"
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/sirupsen/logrus"
)

// Integration test - requires Docker to be running
// Tests that Annotations are actually passed to Docker and stored in container HostConfig
func TestRunJob_Annotations_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	endpoint := "unix:///var/run/docker.sock"
	client, err := docker.NewClient(endpoint)
	if err != nil {
		t.Skip("Docker not available, skipping integration test")
	}

	if _, err := client.Info(); err != nil {
		t.Skipf("Docker daemon not reachable: %v", err)
	}

	testCases := []struct {
		name                string
		userAnnotations     []string
		expectedAnnotations map[string]string // Specific annotations to verify (subset of all)
		checkDefaults       bool              // Whether to verify default Ofelia annotations
	}{
		{
			name:                "no_user_annotations_has_defaults",
			userAnnotations:     []string{},
			expectedAnnotations: map[string]string{
				// Defaults will be checked via checkDefaults flag
			},
			checkDefaults: true,
		},
		{
			name: "single_user_annotation",
			userAnnotations: []string{
				"team=platform",
			},
			expectedAnnotations: map[string]string{
				"team": "platform",
			},
			checkDefaults: true,
		},
		{
			name: "multiple_user_annotations",
			userAnnotations: []string{
				"team=data-engineering",
				"project=analytics-pipeline",
				"environment=production",
				"cost-center=12345",
			},
			expectedAnnotations: map[string]string{
				"team":        "data-engineering",
				"project":     "analytics-pipeline",
				"environment": "production",
				"cost-center": "12345",
			},
			checkDefaults: true,
		},
		{
			name: "user_overrides_default_annotation",
			userAnnotations: []string{
				"ofelia.job.name=custom-job-name",
				"team=platform",
			},
			expectedAnnotations: map[string]string{
				"ofelia.job.name": "custom-job-name", // User override
				"team":            "platform",
			},
			checkDefaults: false, // Don't check defaults since we're overriding one
		},
		{
			name: "complex_annotation_values",
			userAnnotations: []string{
				"description=Multi-tenant analytics pipeline for customer data",
				"tags=kubernetes,docker,monitoring",
				"owner-email=platform-team@company.com",
			},
			expectedAnnotations: map[string]string{
				"description": "Multi-tenant analytics pipeline for customer data",
				"tags":        "kubernetes,docker,monitoring",
				"owner-email": "platform-team@company.com",
			},
			checkDefaults: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create RunJob with Annotations
			job := NewRunJob(client)
			job.Name = "test-annotations-job"
			job.Image = "alpine:latest"
			job.Command = "echo 'Testing annotations'"
			job.Delete = "true" // Auto-delete container
			job.Annotations = tc.userAnnotations

			// Create execution context
			execution, err := NewExecution()
			if err != nil {
				t.Fatalf("Failed to create execution: %v", err)
			}

			logger := logrus.New()
			logger.SetLevel(logrus.WarnLevel)

			ctx := &Context{
				Execution: execution,
				Logger:    &LogrusAdapter{Logger: logger},
			}

			// Pull image first
			imageOps := job.dockerOps.NewImageOperations()
			imageOps.logger = ctx.Logger
			if err := imageOps.EnsureImage(job.Image, false); err != nil {
				t.Skipf("Failed to ensure image: %v (Docker may need to pull alpine:latest)", err)
			}

			// Build container (this creates it but doesn't start it)
			container, err := job.buildContainer()
			if err != nil {
				t.Fatalf("Failed to build container: %v", err)
			}

			// Ensure cleanup
			defer func() {
				containerOps := job.dockerOps.NewContainerLifecycle()
				containerOps.RemoveContainer(container.ID, true)
			}()

			// Inspect the created container to verify annotations
			inspected, err := client.InspectContainer(container.ID)
			if err != nil {
				t.Fatalf("Failed to inspect container: %v", err)
			}

			// Check that HostConfig.Annotations exists and contains expected values
			if inspected.HostConfig == nil {
				t.Fatal("Container HostConfig is nil")
			}

			annotations := inspected.HostConfig.Annotations
			if annotations == nil {
				t.Fatal("Container HostConfig.Annotations is nil")
			}

			// Verify expected user annotations
			for key, expectedValue := range tc.expectedAnnotations {
				if actualValue, ok := annotations[key]; !ok {
					t.Errorf("Expected annotation %q not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("Annotation %q: expected %q, got %q", key, expectedValue, actualValue)
				}
			}

			// Verify default Ofelia annotations if requested
			if tc.checkDefaults {
				defaultKeys := []string{
					"ofelia.job.name",
					"ofelia.job.type",
					"ofelia.execution.time",
					"ofelia.scheduler.host",
					"ofelia.version",
				}

				for _, key := range defaultKeys {
					if _, ok := annotations[key]; !ok {
						t.Errorf("Expected default annotation %q not found", key)
					}
				}

				// Check specific default values
				if annotations["ofelia.job.name"] != job.Name {
					t.Errorf("Default annotation ofelia.job.name: expected %q, got %q",
						job.Name, annotations["ofelia.job.name"])
				}

				if annotations["ofelia.job.type"] != "run" {
					t.Errorf("Default annotation ofelia.job.type: expected 'run', got %q",
						annotations["ofelia.job.type"])
				}

				// Verify execution time is valid RFC3339
				if execTime, ok := annotations["ofelia.execution.time"]; !ok {
					t.Error("Default annotation ofelia.execution.time not found")
				} else if _, err := time.Parse(time.RFC3339, execTime); err != nil {
					t.Errorf("Default annotation ofelia.execution.time is not valid RFC3339: %v", err)
				}
			}

			// Log annotations for debugging
			t.Logf("Container annotations (%d total):", len(annotations))
			for k, v := range annotations {
				t.Logf("  %s=%s", k, v)
			}
		})
	}
}

// Integration test to verify annotations work end-to-end with actual job execution
func TestRunJob_Annotations_EndToEnd_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	endpoint := "unix:///var/run/docker.sock"
	client, err := docker.NewClient(endpoint)
	if err != nil {
		t.Skip("Docker not available, skipping integration test")
	}

	if _, err := client.Info(); err != nil {
		t.Skipf("Docker daemon not reachable: %v", err)
	}

	t.Run("full_job_run_with_annotations", func(t *testing.T) {
		// Create RunJob with comprehensive annotations
		job := NewRunJob(client)
		job.Name = "annotation-end-to-end-test"
		job.Image = "alpine:latest"
		job.Command = "echo 'Job with annotations completed'"
		job.Delete = "true"
		job.Annotations = []string{
			"test-case=end-to-end",
			"team=qa",
			"automated=true",
		}

		// Create execution context
		execution, err := NewExecution()
		if err != nil {
			t.Fatalf("Failed to create execution: %v", err)
		}

		logger := logrus.New()
		logger.SetLevel(logrus.WarnLevel)

		ctx := &Context{
			Execution: execution,
			Logger:    &LogrusAdapter{Logger: logger},
			Job:       job,
		}

		// Run the complete job (this will create, start, wait, and delete the container)
		err = job.Run(ctx)
		if err != nil {
			t.Fatalf("Job execution failed: %v", err)
		}

		// Verify job output contains expected message
		stdout := execution.GetStdout()
		if !strings.Contains(stdout, "Job with annotations completed") {
			t.Errorf("Expected output not found. Got: %s", stdout)
		}

		// Container should be deleted due to Delete=true
		// If we got here without errors, annotations were successfully used
		t.Log("Job with annotations executed successfully")
	})
}
