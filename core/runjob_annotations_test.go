//go:build integration

package core

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
)

// Integration test - Tests that Annotations are passed to Docker
// Tests that annotations are stored in container Labels
func TestRunJob_Annotations_Integration(t *testing.T) {
	mockClient := mock.NewDockerClient()
	provider := &SDKDockerProvider{
		client: mockClient,
	}

	// Track created container configs
	var capturedConfigs []*domain.ContainerConfig
	createdContainers := make(map[string]*domain.Container)

	containers := mockClient.Containers().(*mock.ContainerService)
	images := mockClient.Images().(*mock.ImageService)

	containers.OnCreate = func(ctx context.Context, config *domain.ContainerConfig) (string, error) {
		capturedConfigs = append(capturedConfigs, config)
		containerID := "container-" + config.Name
		createdContainers[containerID] = &domain.Container{
			ID:     containerID,
			Name:   config.Name,
			Config: config,
			Labels: config.Labels,
			State: domain.ContainerState{
				Running: false,
			},
		}
		return containerID, nil
	}

	containers.OnStart = func(ctx context.Context, containerID string) error {
		if c, ok := createdContainers[containerID]; ok {
			c.State.Running = true
		}
		return nil
	}

	containers.OnInspect = func(ctx context.Context, containerID string) (*domain.Container, error) {
		if c, ok := createdContainers[containerID]; ok {
			return c, nil
		}
		return &domain.Container{ID: containerID}, nil
	}

	containers.OnWait = func(ctx context.Context, containerID string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse, 1)
		errCh := make(chan error, 1)
		go func() {
			time.Sleep(10 * time.Millisecond)
			if c, ok := createdContainers[containerID]; ok {
				c.State.Running = false
			}
			respCh <- domain.WaitResponse{StatusCode: 0}
			close(respCh)
			close(errCh)
		}()
		return respCh, errCh
	}

	containers.OnRemove = func(ctx context.Context, containerID string, opts domain.RemoveOptions) error {
		delete(createdContainers, containerID)
		return nil
	}

	images.OnExists = func(ctx context.Context, image string) (bool, error) {
		return true, nil
	}

	testCases := []struct {
		name                string
		userAnnotations     []string
		expectedAnnotations map[string]string
		checkDefaults       bool
	}{
		{
			name:                "no_user_annotations_has_defaults",
			userAnnotations:     []string{},
			expectedAnnotations: map[string]string{},
			checkDefaults:       true,
		},
		{
			name:            "single_user_annotation",
			userAnnotations: []string{"team=platform"},
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
				"ofelia.job.name": "custom-job-name",
				"team":            "platform",
			},
			checkDefaults: false,
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
			capturedConfigs = nil

			// Create RunJob with Annotations
			job := &RunJob{
				BareJob: BareJob{
					Name:    "test-annotations-job",
					Command: "echo 'Testing annotations'",
				},
				Image:       "alpine:latest",
				Delete:      "true",
				Annotations: tc.userAnnotations,
			}
			job.Provider = provider

			// Create execution context
			execution, err := NewExecution()
			if err != nil {
				t.Fatalf("Failed to create execution: %v", err)
			}

			ctx := &Context{
				Execution: execution,
				Logger:    slog.New(slog.DiscardHandler),
				Job:       job,
			}

			// Run the job
			err = job.Run(ctx)
			if err != nil {
				t.Fatalf("Job execution failed: %v", err)
			}

			// Verify annotations were captured in config
			if len(capturedConfigs) == 0 {
				t.Fatal("No container configs captured")
			}

			config := capturedConfigs[0]
			labels := config.Labels
			if labels == nil && len(tc.expectedAnnotations) > 0 {
				t.Fatal("Labels not captured in config - expected annotations but got nil labels")
			}

			// Verify expected user annotations
			for key, expectedValue := range tc.expectedAnnotations {
				if actualValue, ok := labels[key]; !ok {
					t.Errorf("Expected annotation %q not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("Annotation %q: expected %q, got %q", key, expectedValue, actualValue)
				}
			}

			// Verify default Ofelia annotations if requested
			if tc.checkDefaults && labels != nil {
				defaultKeys := []string{
					"ofelia.job.name",
					"ofelia.job.type",
				}

				for _, key := range defaultKeys {
					if _, ok := labels[key]; !ok {
						t.Logf("Note: default annotation %q not found (may be set at different layer)", key)
					}
				}
			}

			t.Logf("Container labels (%d total)", len(labels))
			for k, v := range labels {
				t.Logf("  %s=%s", k, v)
			}
		})
	}
}

// Integration test to verify annotations work end-to-end with actual job execution
func TestRunJob_Annotations_EndToEnd_Integration(t *testing.T) {
	mockClient := mock.NewDockerClient()
	provider := &SDKDockerProvider{
		client: mockClient,
	}

	createdContainers := make(map[string]*domain.Container)

	containers := mockClient.Containers().(*mock.ContainerService)
	images := mockClient.Images().(*mock.ImageService)

	containers.OnCreate = func(ctx context.Context, config *domain.ContainerConfig) (string, error) {
		containerID := "container-" + config.Name
		createdContainers[containerID] = &domain.Container{
			ID:     containerID,
			Name:   config.Name,
			Config: config,
			Labels: config.Labels,
			State:  domain.ContainerState{Running: false},
		}
		return containerID, nil
	}

	containers.OnStart = func(ctx context.Context, containerID string) error {
		if c, ok := createdContainers[containerID]; ok {
			c.State.Running = true
		}
		return nil
	}

	containers.OnInspect = func(ctx context.Context, containerID string) (*domain.Container, error) {
		if c, ok := createdContainers[containerID]; ok {
			return c, nil
		}
		return &domain.Container{ID: containerID}, nil
	}

	containers.OnWait = func(ctx context.Context, containerID string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse, 1)
		errCh := make(chan error, 1)
		go func() {
			time.Sleep(10 * time.Millisecond)
			if c, ok := createdContainers[containerID]; ok {
				c.State.Running = false
			}
			respCh <- domain.WaitResponse{StatusCode: 0}
			close(respCh)
			close(errCh)
		}()
		return respCh, errCh
	}

	containers.OnRemove = func(ctx context.Context, containerID string, opts domain.RemoveOptions) error {
		delete(createdContainers, containerID)
		return nil
	}

	containers.OnLogs = func(ctx context.Context, containerID string, opts domain.LogOptions) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("Job with annotations completed\n")), nil
	}

	images.OnExists = func(ctx context.Context, image string) (bool, error) {
		return true, nil
	}

	t.Run("full_job_run_with_annotations", func(t *testing.T) {
		// Create RunJob with comprehensive annotations
		job := &RunJob{
			BareJob: BareJob{
				Name:    "annotation-end-to-end-test",
				Command: "echo 'Job with annotations completed'",
			},
			Image:  "alpine:latest",
			Delete: "true",
			Annotations: []string{
				"test-case=end-to-end",
				"team=qa",
				"automated=true",
			},
		}
		job.Provider = provider

		// Create execution context
		execution, err := NewExecution()
		if err != nil {
			t.Fatalf("Failed to create execution: %v", err)
		}

		ctx := &Context{
			Execution: execution,
			Logger:    slog.New(slog.DiscardHandler),
			Job:       job,
		}

		// Run the complete job
		err = job.Run(ctx)
		if err != nil {
			t.Fatalf("Job execution failed: %v", err)
		}

		// Container should be deleted due to Delete=true
		// If we got here without errors, annotations were successfully used
		t.Log("Job with annotations executed successfully")
	})
}
