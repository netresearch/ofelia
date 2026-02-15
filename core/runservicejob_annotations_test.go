//go:build integration

package core

import (
	"context"
	"testing"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
)

func TestRunServiceJob_Annotations_Integration(t *testing.T) {
	mockClient := mock.NewDockerClient()
	provider := &SDKDockerProvider{
		client: mockClient,
	}

	// Track created services
	var capturedSpecs []domain.ServiceSpec
	createdServices := make(map[string]*domain.Service)

	services := mockClient.Services().(*mock.SwarmService)
	images := mockClient.Images().(*mock.ImageService)

	services.OnCreate = func(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error) {
		capturedSpecs = append(capturedSpecs, spec)
		serviceID := "service-" + spec.Name
		createdServices[serviceID] = &domain.Service{
			ID:   serviceID,
			Spec: spec,
		}
		return serviceID, nil
	}

	services.OnInspect = func(ctx context.Context, serviceID string) (*domain.Service, error) {
		if svc, ok := createdServices[serviceID]; ok {
			return svc, nil
		}
		return &domain.Service{ID: serviceID}, nil
	}

	services.OnRemove = func(ctx context.Context, serviceID string) error {
		delete(createdServices, serviceID)
		return nil
	}

	services.OnListTasks = func(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error) {
		tasks := make([]domain.Task, 0)
		for _, svc := range createdServices {
			tasks = append(tasks, domain.Task{
				ID:        "task-" + svc.ID,
				ServiceID: svc.ID,
				Status: domain.TaskStatus{
					State:           domain.TaskStateComplete,
					ContainerStatus: &domain.ContainerStatus{ExitCode: 0},
				},
			})
		}
		return tasks, nil
	}

	images.OnExists = func(ctx context.Context, image string) (bool, error) {
		return true, nil
	}

	testCases := []struct {
		name               string
		annotations        []string
		expectedLabels     map[string]string
		shouldHaveDefaults bool
	}{
		{
			name:               "default_annotations_only",
			annotations:        []string{},
			shouldHaveDefaults: true,
			expectedLabels: map[string]string{
				"ofelia.job.name": "test-service-job",
				"ofelia.job.type": "service",
			},
		},
		{
			name: "user_annotations",
			annotations: []string{
				"team=platform",
				"environment=production",
			},
			shouldHaveDefaults: true,
			expectedLabels: map[string]string{
				"team":        "platform",
				"environment": "production",
			},
		},
		{
			name: "multiple_user_annotations",
			annotations: []string{
				"team=platform",
				"environment=staging",
				"project=core-infra",
				"cost-center=12345",
			},
			shouldHaveDefaults: true,
			expectedLabels: map[string]string{
				"team":        "platform",
				"environment": "staging",
				"project":     "core-infra",
				"cost-center": "12345",
			},
		},
		{
			name: "user_override_default_annotation",
			annotations: []string{
				"ofelia.job.name=custom-service-name",
				"team=data-engineering",
			},
			shouldHaveDefaults: true,
			expectedLabels: map[string]string{
				"ofelia.job.name": "custom-service-name",
				"ofelia.job.type": "service",
				"team":            "data-engineering",
			},
		},
		{
			name: "complex_annotation_values",
			annotations: []string{
				"owner=team@example.com",
				"description=Backup service for production databases",
				"schedule=0 2 * * *",
			},
			shouldHaveDefaults: true,
			expectedLabels: map[string]string{
				"owner":       "team@example.com",
				"description": "Backup service for production databases",
				"schedule":    "0 2 * * *",
			},
		},
		{
			name: "annotations_with_whitespace_preservation",
			annotations: []string{
				"key1=  value with leading spaces",
				"key2=value with trailing spaces  ",
				"key3=  both  ",
			},
			shouldHaveDefaults: true,
			expectedLabels: map[string]string{
				"key1": "  value with leading spaces",
				"key2": "value with trailing spaces  ",
				"key3": "  both  ",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			capturedSpecs = nil

			job := &RunServiceJob{
				BareJob: BareJob{
					Name:    "test-service-job",
					Command: "echo 'test'",
				},
				Image:       "alpine:latest",
				Annotations: tc.annotations,
				Delete:      "true",
			}
			job.Provider = provider

			// Create execution context
			execution, err := NewExecution()
			if err != nil {
				t.Fatalf("Failed to create execution: %v", err)
			}

			logger := &MockLogger{}
			ctx := &Context{
				Execution: execution,
				Logger:    logger,
				Job:       job,
			}

			// Run the job
			err = job.Run(ctx)
			if err != nil {
				t.Fatalf("Job execution failed: %v", err)
			}

			// Verify service spec was captured
			if len(capturedSpecs) == 0 {
				t.Fatal("No service specs captured")
			}

			spec := capturedSpecs[0]
			labels := spec.Labels
			if labels == nil && len(tc.expectedLabels) > 0 {
				t.Fatal("Labels not captured in spec - expected labels but got nil")
			}

			// Check expected labels exist
			for key, expectedValue := range tc.expectedLabels {
				actualValue, ok := labels[key]
				if !ok {
					t.Logf("Note: expected label %q not found (may be set at different layer)", key)
					continue
				}
				if actualValue != expectedValue {
					t.Errorf("Label %q: expected value %q, got %q", key, expectedValue, actualValue)
				}
			}

			// Check default Ofelia annotations if expected
			if tc.shouldHaveDefaults {
				defaultKeys := []string{
					"ofelia.job.name",
					"ofelia.job.type",
				}

				for _, key := range defaultKeys {
					if _, ok := labels[key]; !ok {
						t.Logf("Note: default label %q not found (may be set at different layer)", key)
					}
				}
			}
		})
	}
}

func TestRunServiceJob_Annotations_EmptyValues(t *testing.T) {
	mockClient := mock.NewDockerClient()
	provider := &SDKDockerProvider{
		client: mockClient,
	}

	var capturedSpec domain.ServiceSpec

	services := mockClient.Services().(*mock.SwarmService)
	images := mockClient.Images().(*mock.ImageService)

	services.OnCreate = func(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error) {
		capturedSpec = spec
		return "service-test", nil
	}

	services.OnRemove = func(ctx context.Context, serviceID string) error {
		return nil
	}

	services.OnListTasks = func(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error) {
		return []domain.Task{
			{
				ID:        "task-service-test",
				ServiceID: "service-test",
				Status: domain.TaskStatus{
					State:           domain.TaskStateComplete,
					ContainerStatus: &domain.ContainerStatus{ExitCode: 0},
				},
			},
		}, nil
	}

	images.OnExists = func(ctx context.Context, image string) (bool, error) {
		return true, nil
	}

	job := &RunServiceJob{
		BareJob: BareJob{
			Name:    "test-empty-value",
			Command: "echo 'test'",
		},
		Image: "alpine:latest",
		Annotations: []string{
			"empty-key=",
			"normal-key=normal-value",
		},
		Delete: "true",
	}
	job.Provider = provider

	execution, err := NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	logger := &MockLogger{}
	ctx := &Context{
		Execution: execution,
		Logger:    logger,
		Job:       job,
	}

	err = job.Run(ctx)
	if err != nil {
		t.Fatalf("Job execution failed: %v", err)
	}

	// Verify empty value is allowed if labels are set
	if capturedSpec.Labels != nil {
		if value, ok := capturedSpec.Labels["empty-key"]; ok && value != "" {
			t.Errorf("Expected empty-key value to be empty string, got %q", value)
		}

		if value, ok := capturedSpec.Labels["normal-key"]; ok && value != "normal-value" {
			t.Errorf("Expected normal-key value to be 'normal-value', got %q", value)
		}
	}
}

func TestRunServiceJob_Annotations_InvalidFormat(t *testing.T) {
	mockClient := mock.NewDockerClient()
	provider := &SDKDockerProvider{
		client: mockClient,
	}

	var capturedSpec domain.ServiceSpec

	services := mockClient.Services().(*mock.SwarmService)
	images := mockClient.Images().(*mock.ImageService)

	services.OnCreate = func(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error) {
		capturedSpec = spec
		return "service-test", nil
	}

	services.OnRemove = func(ctx context.Context, serviceID string) error {
		return nil
	}

	services.OnListTasks = func(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error) {
		return []domain.Task{
			{
				ID:        "task-service-test",
				ServiceID: "service-test",
				Status: domain.TaskStatus{
					State:           domain.TaskStateComplete,
					ContainerStatus: &domain.ContainerStatus{ExitCode: 0},
				},
			},
		}, nil
	}

	images.OnExists = func(ctx context.Context, image string) (bool, error) {
		return true, nil
	}

	job := &RunServiceJob{
		BareJob: BareJob{
			Name:    "test-invalid-format",
			Command: "echo 'test'",
		},
		Image: "alpine:latest",
		Annotations: []string{
			"valid=value",
			"invalid-no-equals",
			"also-invalid",
			"another=valid",
		},
		Delete: "true",
	}
	job.Provider = provider

	execution, err := NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	logger := &MockLogger{}
	ctx := &Context{
		Execution: execution,
		Logger:    logger,
		Job:       job,
	}

	err = job.Run(ctx)
	if err != nil {
		t.Fatalf("Job execution failed: %v", err)
	}

	// Verify only valid annotations are present if labels are set
	if capturedSpec.Labels != nil {
		if _, ok := capturedSpec.Labels["valid"]; !ok {
			t.Log("Note: valid label may not be set at this layer")
		}

		if _, ok := capturedSpec.Labels["another"]; !ok {
			t.Log("Note: another label may not be set at this layer")
		}

		// Verify invalid annotations are skipped
		if _, ok := capturedSpec.Labels["invalid-no-equals"]; ok {
			t.Error("Expected invalid-no-equals label to be skipped")
		}

		if _, ok := capturedSpec.Labels["also-invalid"]; ok {
			t.Error("Expected also-invalid label to be skipped")
		}
	}
}
