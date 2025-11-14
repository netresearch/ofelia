package core

import (
	"testing"
	"time"
)

func TestParseAnnotations(t *testing.T) {
	testCases := []struct {
		name     string
		input    []string
		expected map[string]string
	}{
		{
			name:     "empty_annotations",
			input:    []string{},
			expected: map[string]string{},
		},
		{
			name:  "single_annotation",
			input: []string{"key=value"},
			expected: map[string]string{
				"key": "value",
			},
		},
		{
			name: "multiple_annotations",
			input: []string{
				"team=platform",
				"environment=production",
				"project=core-infra",
			},
			expected: map[string]string{
				"team":        "platform",
				"environment": "production",
				"project":     "core-infra",
			},
		},
		{
			name: "annotations_with_equals_in_value",
			input: []string{
				"formula=e=mc^2",
				"equation=x+y=10",
			},
			expected: map[string]string{
				"formula":  "e=mc^2",
				"equation": "x+y=10",
			},
		},
		{
			name: "annotations_with_spaces",
			input: []string{
				"  key1  =  value1  ",
				"key2=value with spaces",
				"key3=  leading spaces preserved",
				"key4=trailing spaces preserved  ",
			},
			expected: map[string]string{
				"key1": "  value1  ",                  // value whitespace preserved
				"key2": "value with spaces",           // internal spaces always preserved
				"key3": "  leading spaces preserved",  // leading spaces in value preserved
				"key4": "trailing spaces preserved  ", // trailing spaces in value preserved
			},
		},
		{
			name: "invalid_annotations_skipped",
			input: []string{
				"valid=value",
				"invalid-no-equals",
				"also-invalid",
				"another=valid",
			},
			expected: map[string]string{
				"valid":   "value",
				"another": "valid",
			},
		},
		{
			name: "empty_key_skipped",
			input: []string{
				"=value",
				"valid=value",
			},
			expected: map[string]string{
				"valid": "value",
			},
		},
		{
			name: "empty_value_allowed",
			input: []string{
				"key=",
			},
			expected: map[string]string{
				"key": "",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseAnnotations(tc.input)

			if len(result) != len(tc.expected) {
				t.Errorf("Expected %d annotations, got %d", len(tc.expected), len(result))
				return
			}

			for key, expectedValue := range tc.expected {
				if actualValue, ok := result[key]; !ok {
					t.Errorf("Expected key %q not found in result", key)
				} else if actualValue != expectedValue {
					t.Errorf("For key %q: expected value %q, got %q", key, expectedValue, actualValue)
				}
			}
		})
	}
}

func TestGetDefaultAnnotations(t *testing.T) {
	jobName := "test-job"
	jobType := "run"

	result := getDefaultAnnotations(jobName, jobType)

	// Check required keys exist
	requiredKeys := []string{
		"ofelia.job.name",
		"ofelia.job.type",
		"ofelia.execution.time",
		"ofelia.scheduler.host",
		"ofelia.version",
	}

	for _, key := range requiredKeys {
		if _, ok := result[key]; !ok {
			t.Errorf("Expected default annotation %q not found", key)
		}
	}

	// Check specific values
	if result["ofelia.job.name"] != jobName {
		t.Errorf("Expected job name %q, got %q", jobName, result["ofelia.job.name"])
	}

	if result["ofelia.job.type"] != jobType {
		t.Errorf("Expected job type %q, got %q", jobType, result["ofelia.job.type"])
	}

	// Check execution time is valid RFC3339 format
	if _, err := time.Parse(time.RFC3339, result["ofelia.execution.time"]); err != nil {
		t.Errorf("Execution time %q is not valid RFC3339 format: %v", result["ofelia.execution.time"], err)
	}

	// Hostname should not be empty
	if result["ofelia.scheduler.host"] == "" {
		t.Error("Expected scheduler host to be set")
	}
}

func TestMergeAnnotations(t *testing.T) {
	testCases := []struct {
		name     string
		user     []string
		defaults map[string]string
		expected map[string]string
	}{
		{
			name: "user_overrides_defaults",
			user: []string{
				"ofelia.job.name=custom-name",
				"team=platform",
			},
			defaults: map[string]string{
				"ofelia.job.name": "default-name",
				"ofelia.job.type": "run",
			},
			expected: map[string]string{
				"ofelia.job.name": "custom-name", // User override
				"ofelia.job.type": "run",         // Default preserved
				"team":            "platform",    // User addition
			},
		},
		{
			name: "empty_user_annotations",
			user: []string{},
			defaults: map[string]string{
				"ofelia.job.name": "test-job",
				"ofelia.job.type": "run",
			},
			expected: map[string]string{
				"ofelia.job.name": "test-job",
				"ofelia.job.type": "run",
			},
		},
		{
			name: "empty_defaults",
			user: []string{
				"team=platform",
				"env=prod",
			},
			defaults: map[string]string{},
			expected: map[string]string{
				"team": "platform",
				"env":  "prod",
			},
		},
		{
			name: "complex_merge",
			user: []string{
				"ofelia.execution.time=2024-01-01T00:00:00Z",
				"team=data-engineering",
				"project=analytics",
				"cost-center=12345",
			},
			defaults: map[string]string{
				"ofelia.job.name":       "backup-job",
				"ofelia.job.type":       "run",
				"ofelia.execution.time": "2024-01-02T00:00:00Z",
				"ofelia.scheduler.host": "prod-01",
			},
			expected: map[string]string{
				"ofelia.job.name":       "backup-job",
				"ofelia.job.type":       "run",
				"ofelia.execution.time": "2024-01-01T00:00:00Z", // User override
				"ofelia.scheduler.host": "prod-01",
				"team":                  "data-engineering",
				"project":               "analytics",
				"cost-center":           "12345",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := mergeAnnotations(tc.user, tc.defaults)

			if len(result) != len(tc.expected) {
				t.Errorf("Expected %d annotations, got %d", len(tc.expected), len(result))
				t.Logf("Expected: %v", tc.expected)
				t.Logf("Got: %v", result)
				return
			}

			for key, expectedValue := range tc.expected {
				if actualValue, ok := result[key]; !ok {
					t.Errorf("Expected key %q not found in result", key)
				} else if actualValue != expectedValue {
					t.Errorf("For key %q: expected value %q, got %q", key, expectedValue, actualValue)
				}
			}
		})
	}
}
