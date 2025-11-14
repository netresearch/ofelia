//go:build integration
// +build integration

package core

import (
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

func TestRunServiceJob_Annotations_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	endpoint := "unix:///var/run/docker.sock"
	client, err := docker.NewClient(endpoint)
	if err != nil {
		t.Skip("Docker not available, skipping integration test")
	}

	// Check if Swarm is initialized
	swarmInfo, err := client.Info()
	if err != nil {
		t.Skip("Cannot get Docker info, skipping integration test")
	}

	if swarmInfo.Swarm.LocalNodeState != "active" {
		// Try to initialize Swarm for testing
		_, err := client.InitSwarm(docker.InitSwarmOptions{})
		if err != nil {
			t.Skipf("Swarm not initialized and cannot initialize: %v", err)
		}
		// Give Swarm time to initialize
		time.Sleep(2 * time.Second)
	}

	testCases := []struct {
		name               string
		annotations        []string
		expectedLabels     map[string]string // For service jobs, annotations are stored as labels
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
				"ofelia.job.name": "custom-service-name", // User override
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
			job := &RunServiceJob{
				Client: client,
			}
			job.Name = "test-service-job"
			job.Image = "alpine:latest"
			job.Command = "echo 'test'"
			job.Annotations = tc.annotations
			job.Delete = "true" // Auto-cleanup

			// Build the service (this also creates it in Docker)
			service, err := job.buildService()
			if err != nil {
				t.Fatalf("Failed to build service: %v", err)
			}

			// Cleanup: Remove the service
			defer func() {
				removeErr := client.RemoveService(docker.RemoveServiceOptions{
					ID: service.ID,
				})
				if removeErr != nil {
					t.Logf("Warning: Failed to remove service %s: %v", service.ID, removeErr)
				}
			}()

			// Inspect the created service to verify labels are set correctly
			inspectedService, err := client.InspectService(service.ID)
			if err != nil {
				t.Fatalf("Failed to inspect service: %v", err)
			}

			// Verify annotations are stored as service labels
			if inspectedService.Spec.Labels == nil {
				t.Fatal("Expected service labels to be set, got nil")
			}

			// Check expected labels exist
			for key, expectedValue := range tc.expectedLabels {
				actualValue, ok := inspectedService.Spec.Labels[key]
				if !ok {
					t.Errorf("Expected label %q not found in service labels", key)
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
					"ofelia.execution.time",
					"ofelia.scheduler.host",
					"ofelia.version",
				}

				for _, key := range defaultKeys {
					if _, ok := inspectedService.Spec.Labels[key]; !ok {
						t.Errorf("Expected default label %q not found in service labels", key)
					}
				}

				// Verify ofelia.job.type is always "service"
				if inspectedService.Spec.Labels["ofelia.job.type"] != "service" {
					t.Errorf("Expected ofelia.job.type to be 'service', got %q", inspectedService.Spec.Labels["ofelia.job.type"])
				}

				// Verify execution time is valid RFC3339 format
				if execTime, ok := inspectedService.Spec.Labels["ofelia.execution.time"]; ok {
					if _, err := time.Parse(time.RFC3339, execTime); err != nil {
						t.Errorf("Execution time %q is not valid RFC3339 format: %v", execTime, err)
					}
				}
			}
		})
	}
}

func TestRunServiceJob_Annotations_EmptyValues(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	endpoint := "unix:///var/run/docker.sock"
	client, err := docker.NewClient(endpoint)
	if err != nil {
		t.Skip("Docker not available, skipping integration test")
	}

	// Check Swarm
	swarmInfo, err := client.Info()
	if err != nil {
		t.Skip("Cannot get Docker info, skipping integration test")
	}

	if swarmInfo.Swarm.LocalNodeState != "active" {
		t.Skip("Swarm not initialized, skipping service annotation test")
	}

	job := &RunServiceJob{
		Client: client,
	}
	job.Name = "test-empty-value"
	job.Image = "alpine:latest"
	job.Command = "echo 'test'"
	job.Annotations = []string{
		"empty-key=",
		"normal-key=normal-value",
	}
	job.Delete = "true"

	service, err := job.buildService()
	if err != nil {
		t.Fatalf("Failed to build service: %v", err)
	}

	// Cleanup
	defer func() {
		removeErr := client.RemoveService(docker.RemoveServiceOptions{
			ID: service.ID,
		})
		if removeErr != nil {
			t.Logf("Warning: Failed to remove service %s: %v", service.ID, removeErr)
		}
	}()

	// Inspect the created service
	inspectedService, err := client.InspectService(service.ID)
	if err != nil {
		t.Fatalf("Failed to inspect service: %v", err)
	}

	// Verify empty value is allowed
	if value, ok := inspectedService.Spec.Labels["empty-key"]; !ok {
		t.Error("Expected empty-key label to exist")
	} else if value != "" {
		t.Errorf("Expected empty-key value to be empty string, got %q", value)
	}

	// Verify normal key works
	if value, ok := inspectedService.Spec.Labels["normal-key"]; !ok {
		t.Error("Expected normal-key label to exist")
	} else if value != "normal-value" {
		t.Errorf("Expected normal-key value to be 'normal-value', got %q", value)
	}
}

func TestRunServiceJob_Annotations_InvalidFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	endpoint := "unix:///var/run/docker.sock"
	client, err := docker.NewClient(endpoint)
	if err != nil {
		t.Skip("Docker not available, skipping integration test")
	}

	// Check Swarm
	swarmInfo, err := client.Info()
	if err != nil {
		t.Skip("Cannot get Docker info, skipping integration test")
	}

	if swarmInfo.Swarm.LocalNodeState != "active" {
		t.Skip("Swarm not initialized, skipping service annotation test")
	}

	job := &RunServiceJob{
		Client: client,
	}
	job.Name = "test-invalid-format"
	job.Image = "alpine:latest"
	job.Command = "echo 'test'"
	job.Annotations = []string{
		"valid=value",
		"invalid-no-equals",
		"also-invalid",
		"another=valid",
	}
	job.Delete = "true"

	service, err := job.buildService()
	if err != nil {
		t.Fatalf("Failed to build service: %v", err)
	}

	// Cleanup
	defer func() {
		removeErr := client.RemoveService(docker.RemoveServiceOptions{
			ID: service.ID,
		})
		if removeErr != nil {
			t.Logf("Warning: Failed to remove service %s: %v", service.ID, removeErr)
		}
	}()

	// Inspect the created service
	inspectedService, err := client.InspectService(service.ID)
	if err != nil {
		t.Fatalf("Failed to inspect service: %v", err)
	}

	// Verify only valid annotations are present
	if _, ok := inspectedService.Spec.Labels["valid"]; !ok {
		t.Error("Expected valid label to exist")
	}

	if _, ok := inspectedService.Spec.Labels["another"]; !ok {
		t.Error("Expected another label to exist")
	}

	// Verify invalid annotations are skipped
	if _, ok := inspectedService.Spec.Labels["invalid-no-equals"]; ok {
		t.Error("Expected invalid-no-equals label to be skipped")
	}

	if _, ok := inspectedService.Spec.Labels["also-invalid"]; ok {
		t.Error("Expected also-invalid label to be skipped")
	}
}
