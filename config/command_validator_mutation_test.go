package config

import (
	"strings"
	"testing"
)

// Tests targeting surviving CONDITIONALS_BOUNDARY mutations in command_validator.go

func TestValidateServiceName_BoundaryConditions(t *testing.T) {
	t.Parallel()
	v := NewCommandValidator()

	testCases := []struct {
		name        string
		serviceName string
		wantErr     bool
		desc        string
	}{
		// Targeting line 46: if len(service) > 255
		{
			name:        "exactly_255_chars",
			serviceName: strings.Repeat("a", 255),
			wantErr:     false,
			desc:        "Service name exactly 255 chars should pass",
		},
		{
			name:        "at_254_chars",
			serviceName: strings.Repeat("a", 254),
			wantErr:     false,
			desc:        "Service name at 254 chars should pass",
		},
		{
			name:        "at_256_chars",
			serviceName: strings.Repeat("a", 256),
			wantErr:     true,
			desc:        "Service name at 256 chars should fail",
		},
		// Edge cases
		{
			name:        "empty_service_name",
			serviceName: "",
			wantErr:     true,
			desc:        "Empty service name should fail",
		},
		{
			name:        "single_char",
			serviceName: "a",
			wantErr:     false,
			desc:        "Single char service name should pass",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := v.ValidateServiceName(tc.serviceName)
			if (err != nil) != tc.wantErr {
				t.Errorf("%s: got error %v, wantErr %v", tc.desc, err, tc.wantErr)
			}
		})
	}
}

func TestValidateFilePath_BoundaryConditions(t *testing.T) {
	t.Parallel()
	v := NewCommandValidator()

	testCases := []struct {
		name    string
		path    string
		wantErr bool
		desc    string
	}{
		// Targeting line 71: if len(path) > 4096
		{
			name:    "exactly_4096_chars",
			path:    "/home/" + strings.Repeat("a", 4090),
			wantErr: false,
			desc:    "Path exactly 4096 chars should pass",
		},
		{
			name:    "at_4095_chars",
			path:    "/home/" + strings.Repeat("a", 4089),
			wantErr: false,
			desc:    "Path at 4095 chars should pass",
		},
		{
			name:    "at_4097_chars",
			path:    "/home/" + strings.Repeat("a", 4091),
			wantErr: true,
			desc:    "Path at 4097 chars should fail",
		},
		// Edge cases
		{
			name:    "empty_path",
			path:    "",
			wantErr: true,
			desc:    "Empty path should fail",
		},
		{
			name:    "single_char_path",
			path:    "a",
			wantErr: false,
			desc:    "Single char path should pass",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := v.ValidateFilePath(tc.path)
			if (err != nil) != tc.wantErr {
				t.Errorf("%s: got error %v, wantErr %v", tc.desc, err, tc.wantErr)
			}
		})
	}
}

func TestValidateCommandArgs_BoundaryConditions(t *testing.T) {
	t.Parallel()
	v := NewCommandValidator()

	testCases := []struct {
		name    string
		args    []string
		wantErr bool
		desc    string
	}{
		// Targeting line 104: if len(arg) > 4096
		{
			name:    "arg_exactly_4096",
			args:    []string{strings.Repeat("a", 4096)},
			wantErr: false,
			desc:    "Argument exactly 4096 chars should pass",
		},
		{
			name:    "arg_at_4095",
			args:    []string{strings.Repeat("a", 4095)},
			wantErr: false,
			desc:    "Argument at 4095 chars should pass",
		},
		{
			name:    "arg_at_4097",
			args:    []string{strings.Repeat("a", 4097)},
			wantErr: true,
			desc:    "Argument at 4097 chars should fail",
		},
		// Multiple args with one at boundary
		{
			name:    "multiple_args_one_at_boundary",
			args:    []string{"valid", strings.Repeat("b", 4096), "also-valid"},
			wantErr: false,
			desc:    "Multiple args with one at 4096 should pass",
		},
		{
			name:    "multiple_args_one_over_boundary",
			args:    []string{"valid", strings.Repeat("b", 4097), "also-valid"},
			wantErr: true,
			desc:    "Multiple args with one over 4096 should fail",
		},
		// Edge cases
		{
			name:    "empty_args",
			args:    []string{},
			wantErr: false,
			desc:    "Empty args should pass",
		},
		{
			name:    "single_empty_arg",
			args:    []string{""},
			wantErr: false,
			desc:    "Single empty arg should pass",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := v.ValidateCommandArgs(tc.args)
			if (err != nil) != tc.wantErr {
				t.Errorf("%s: got error %v, wantErr %v", tc.desc, err, tc.wantErr)
			}
		})
	}
}

func TestSanitizeCommand_BoundaryConditions(t *testing.T) {
	t.Parallel()
	v := NewCommandValidator()

	testCases := []struct {
		name       string
		input      string
		wantLength int
		desc       string
	}{
		// Targeting line 129: if len(cmd) > 4096
		{
			name:       "cmd_exactly_4096",
			input:      strings.Repeat("a", 4096),
			wantLength: 4096,
			desc:       "Command exactly 4096 chars should not be truncated",
		},
		{
			name:       "cmd_at_4095",
			input:      strings.Repeat("a", 4095),
			wantLength: 4095,
			desc:       "Command at 4095 chars should not be truncated",
		},
		{
			name:       "cmd_at_4097",
			input:      strings.Repeat("a", 4097),
			wantLength: 4096,
			desc:       "Command at 4097 chars should be truncated to 4096",
		},
		{
			name:       "cmd_at_5000",
			input:      strings.Repeat("a", 5000),
			wantLength: 4096,
			desc:       "Command at 5000 chars should be truncated to 4096",
		},
		// Edge cases
		{
			name:       "empty_cmd",
			input:      "",
			wantLength: 0,
			desc:       "Empty command should remain empty",
		},
		{
			name:       "single_char_cmd",
			input:      "a",
			wantLength: 1,
			desc:       "Single char command should not be truncated",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := v.SanitizeCommand(tc.input)
			if len(result) != tc.wantLength {
				t.Errorf("%s: got length %d, want %d", tc.desc, len(result), tc.wantLength)
			}
		})
	}
}
