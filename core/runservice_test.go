package core

import (
	"testing"
)

func TestRunServiceJob_Validate(t *testing.T) {
	testCases := []struct {
		name        string
		image       string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid_with_image",
			image:       "nginx:latest",
			expectError: false,
		},
		{
			name:        "invalid_missing_image",
			image:       "",
			expectError: true,
			errorMsg:    "job-service-run requires 'image' to create a new swarm service",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := &RunServiceJob{
				Image: tc.image,
			}

			err := job.Validate()

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tc.errorMsg != "" && err.Error() != tc.errorMsg {
					t.Errorf("Expected error message %q, got %q", tc.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}
