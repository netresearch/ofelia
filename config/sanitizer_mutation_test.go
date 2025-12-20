package config

import (
	"strings"
	"testing"
)

// Tests targeting surviving CONDITIONALS_BOUNDARY mutations in sanitizer.go

func TestSanitizeString_BoundaryConditions(t *testing.T) {
	t.Parallel()
	s := NewSanitizer()

	testCases := []struct {
		name      string
		input     string
		maxLength int
		wantErr   bool
		desc      string
	}{
		// Targeting line 45: if len(input) > maxLength
		// Test exactly at boundary (should pass)
		{
			name:      "exactly_at_max_length",
			input:     strings.Repeat("a", 100),
			maxLength: 100,
			wantErr:   false,
			desc:      "String exactly at max length should pass",
		},
		// Test one below boundary (should pass)
		{
			name:      "one_below_max_length",
			input:     strings.Repeat("a", 99),
			maxLength: 100,
			wantErr:   false,
			desc:      "String one below max length should pass",
		},
		// Test one above boundary (should fail)
		{
			name:      "one_above_max_length",
			input:     strings.Repeat("a", 101),
			maxLength: 100,
			wantErr:   true,
			desc:      "String one above max length should fail",
		},
		// Edge case: maxLength of 0
		{
			name:      "max_length_zero_empty_input",
			input:     "",
			maxLength: 0,
			wantErr:   false,
			desc:      "Empty string with maxLength 0 should pass",
		},
		{
			name:      "max_length_zero_non_empty_input",
			input:     "a",
			maxLength: 0,
			wantErr:   true,
			desc:      "Non-empty string with maxLength 0 should fail",
		},
		// Edge case: maxLength of 1
		{
			name:      "max_length_one_at_boundary",
			input:     "a",
			maxLength: 1,
			wantErr:   false,
			desc:      "Single char with maxLength 1 should pass",
		},
		{
			name:      "max_length_one_above_boundary",
			input:     "ab",
			maxLength: 1,
			wantErr:   true,
			desc:      "Two chars with maxLength 1 should fail",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := s.SanitizeString(tc.input, tc.maxLength)
			if (err != nil) != tc.wantErr {
				t.Errorf("%s: got error %v, wantErr %v", tc.desc, err, tc.wantErr)
			}
		})
	}
}

func TestValidateEnvironmentVar_BoundaryConditions(t *testing.T) {
	t.Parallel()
	s := NewSanitizer()

	testCases := []struct {
		name    string
		varName string
		value   string
		wantErr bool
		desc    string
	}{
		// Targeting line 145: if len(value) > 4096
		{
			name:    "value_exactly_4096",
			varName: "VALID_VAR",
			value:   strings.Repeat("a", 4096),
			wantErr: false,
			desc:    "Value exactly 4096 chars should pass",
		},
		{
			name:    "value_at_4095",
			varName: "VALID_VAR",
			value:   strings.Repeat("a", 4095),
			wantErr: false,
			desc:    "Value at 4095 chars should pass",
		},
		{
			name:    "value_at_4097",
			varName: "VALID_VAR",
			value:   strings.Repeat("a", 4097),
			wantErr: true,
			desc:    "Value at 4097 chars should fail",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := s.ValidateEnvironmentVar(tc.varName, tc.value)
			if (err != nil) != tc.wantErr {
				t.Errorf("%s: got error %v, wantErr %v", tc.desc, err, tc.wantErr)
			}
		})
	}
}

func TestValidateDockerImage_BoundaryConditions(t *testing.T) {
	t.Parallel()
	s := NewSanitizer()

	testCases := []struct {
		name    string
		image   string
		wantErr bool
		desc    string
	}{
		// Targeting line 204: if len(image) > 255
		{
			name:    "short_valid_image",
			image:   "nginx:latest",
			wantErr: false,
			desc:    "Short valid image should pass",
		},
		{
			name:    "valid_registry_image",
			image:   "docker.io/library/nginx:latest",
			wantErr: false,
			desc:    "Valid registry image should pass",
		},
		// Test length boundaries with simple repository names
		{
			name:    "long_repo_under_limit",
			image:   strings.Repeat("a", 200),
			wantErr: false,
			desc:    "200 char repository name should pass",
		},
		{
			name:    "long_repo_at_255",
			image:   strings.Repeat("a", 255),
			wantErr: false,
			desc:    "255 char repository name should pass",
		},
		{
			name:    "long_repo_at_256",
			image:   strings.Repeat("a", 256),
			wantErr: true,
			desc:    "256 char repository name should fail (exceeds max)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := s.ValidateDockerImage(tc.image)
			if (err != nil) != tc.wantErr {
				t.Errorf("%s: got error %v, wantErr %v", tc.desc, err, tc.wantErr)
			}
		})
	}
}

func TestValidateCronExpression_BoundaryConditions(t *testing.T) {
	t.Parallel()
	s := NewSanitizer()

	testCases := []struct {
		name    string
		expr    string
		wantErr bool
		desc    string
	}{
		// Targeting line 263: if len(fields) == 6 (6-field cron with seconds)
		{
			name:    "five_field_cron",
			expr:    "0 0 * * *",
			wantErr: false,
			desc:    "Standard 5-field cron should pass",
		},
		{
			name:    "six_field_cron_with_seconds",
			expr:    "0 0 0 * * *",
			wantErr: false,
			desc:    "6-field cron with seconds should pass",
		},
		// Targeting line 271: if i >= len(limits) - boundary condition
		{
			name:    "four_field_cron_invalid",
			expr:    "0 0 * *",
			wantErr: true,
			desc:    "4-field cron should fail",
		},
		{
			name:    "seven_field_cron_invalid",
			expr:    "0 0 0 * * * *",
			wantErr: true,
			desc:    "7-field cron should fail",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := s.ValidateCronExpression(tc.expr)
			if (err != nil) != tc.wantErr {
				t.Errorf("%s: got error %v, wantErr %v", tc.desc, err, tc.wantErr)
			}
		})
	}
}

func TestValidateCronRange_BoundaryConditions(t *testing.T) {
	t.Parallel()
	s := NewSanitizer()

	testCases := []struct {
		name    string
		expr    string
		wantErr bool
		desc    string
	}{
		// Targeting lines 317, 322: boundary conditions in range validation
		// startVal < minVal || startVal > maxVal
		{
			name:    "minute_range_start_at_min",
			expr:    "0-30 * * * *",
			wantErr: false,
			desc:    "Range starting at min (0) should pass",
		},
		{
			name:    "minute_range_end_at_max",
			expr:    "0-59 * * * *",
			wantErr: false,
			desc:    "Range ending at max (59) should pass",
		},
		{
			name:    "minute_range_start_below_min",
			expr:    "-1-30 * * * *",
			wantErr: true,
			desc:    "Range starting below min should fail",
		},
		{
			name:    "minute_range_end_above_max",
			expr:    "0-60 * * * *",
			wantErr: true,
			desc:    "Range ending above max should fail",
		},
		// Targeting line 326: if startVal >= endVal
		{
			name:    "range_start_equals_end",
			expr:    "5-5 * * * *",
			wantErr: true,
			desc:    "Range where start equals end should fail",
		},
		{
			name:    "range_start_greater_than_end",
			expr:    "10-5 * * * *",
			wantErr: true,
			desc:    "Range where start > end should fail",
		},
		{
			name:    "range_start_one_less_than_end",
			expr:    "4-5 * * * *",
			wantErr: false,
			desc:    "Range where start is one less than end should pass",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := s.ValidateCronExpression(tc.expr)
			if (err != nil) != tc.wantErr {
				t.Errorf("%s: got error %v, wantErr %v", tc.desc, err, tc.wantErr)
			}
		})
	}
}

func TestValidateCronList_BoundaryConditions(t *testing.T) {
	t.Parallel()
	s := NewSanitizer()

	testCases := []struct {
		name    string
		expr    string
		wantErr bool
		desc    string
	}{
		// Targeting line 363: intVal < minVal || intVal > maxVal
		{
			name:    "list_at_min_boundary",
			expr:    "0,30 * * * *",
			wantErr: false,
			desc:    "List with value at min (0) should pass",
		},
		{
			name:    "list_at_max_boundary",
			expr:    "30,59 * * * *",
			wantErr: false,
			desc:    "List with value at max (59) should pass",
		},
		{
			name:    "list_below_min",
			expr:    "-1,30 * * * *",
			wantErr: true,
			desc:    "List with value below min should fail",
		},
		{
			name:    "list_above_max",
			expr:    "30,60 * * * *",
			wantErr: true,
			desc:    "List with value above max should fail",
		},
		// Test hour field boundaries (0-23)
		{
			name:    "hour_list_at_boundaries",
			expr:    "0 0,23 * * *",
			wantErr: false,
			desc:    "Hour list at 0 and 23 should pass",
		},
		{
			name:    "hour_list_above_max",
			expr:    "0 0,24 * * *",
			wantErr: true,
			desc:    "Hour list with 24 should fail",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := s.ValidateCronExpression(tc.expr)
			if (err != nil) != tc.wantErr {
				t.Errorf("%s: got error %v, wantErr %v", tc.desc, err, tc.wantErr)
			}
		})
	}
}

// TestValidateCronStep_BoundaryConditions tests step expression boundary conditions
// Targets line 349: if err != nil || baseVal < minVal || baseVal > maxVal
func TestValidateCronStep_BoundaryConditions(t *testing.T) {
	t.Parallel()
	s := NewSanitizer()

	testCases := []struct {
		name    string
		expr    string
		wantErr bool
		desc    string
	}{
		// Test */N format (wildcard base - no baseVal check needed)
		{
			name:    "step_wildcard_base",
			expr:    "*/5 * * * *",
			wantErr: false,
			desc:    "Step with wildcard base should pass",
		},
		// Test N/M format with numeric base - targeting line 349 baseVal checks
		{
			name:    "step_base_at_min",
			expr:    "0/5 * * * *",
			wantErr: false,
			desc:    "Step with base at min (0 for minutes) should pass",
		},
		{
			name:    "step_base_at_max",
			expr:    "59/5 * * * *",
			wantErr: false,
			desc:    "Step with base at max (59 for minutes) should pass",
		},
		{
			name:    "step_base_one_above_min",
			expr:    "1/10 * * * *",
			wantErr: false,
			desc:    "Step with base one above min should pass",
		},
		{
			name:    "step_base_one_below_max",
			expr:    "58/5 * * * *",
			wantErr: false,
			desc:    "Step with base one below max should pass",
		},
		// Boundary violations - targeting baseVal > maxVal
		{
			name:    "step_base_above_max_minute",
			expr:    "60/5 * * * *",
			wantErr: true,
			desc:    "Step with base above max (60 for minutes) should fail",
		},
		{
			name:    "step_base_above_max_hour",
			expr:    "* 24/2 * * *",
			wantErr: true,
			desc:    "Step with base above max (24 for hours) should fail",
		},
		// Test step value validation (line 342: stepVal <= 0)
		{
			name:    "step_zero_value",
			expr:    "*/0 * * * *",
			wantErr: true,
			desc:    "Step value of 0 should fail",
		},
		{
			name:    "step_one_value",
			expr:    "*/1 * * * *",
			wantErr: false,
			desc:    "Step value of 1 should pass",
		},
		// Test different cron fields with steps
		{
			name:    "hour_step_base_at_min",
			expr:    "0 0/4 * * *",
			wantErr: false,
			desc:    "Hour step with base at min (0) should pass",
		},
		{
			name:    "hour_step_base_at_max",
			expr:    "0 23/2 * * *",
			wantErr: false,
			desc:    "Hour step with base at max (23) should pass",
		},
		{
			name:    "day_step_base_at_min",
			expr:    "0 0 1/5 * *",
			wantErr: false,
			desc:    "Day step with base at min (1) should pass",
		},
		{
			name:    "day_step_base_at_max",
			expr:    "0 0 31/5 * *",
			wantErr: false,
			desc:    "Day step with base at max (31) should pass",
		},
		{
			name:    "day_step_base_below_min",
			expr:    "0 0 0/5 * *",
			wantErr: true,
			desc:    "Day step with base below min (0 for days) should fail",
		},
		{
			name:    "day_step_base_above_max",
			expr:    "0 0 32/5 * *",
			wantErr: true,
			desc:    "Day step with base above max (32 for days) should fail",
		},
		// Month field steps
		{
			name:    "month_step_base_at_min",
			expr:    "0 0 1 1/3 *",
			wantErr: false,
			desc:    "Month step with base at min (1) should pass",
		},
		{
			name:    "month_step_base_at_max",
			expr:    "0 0 1 12/2 *",
			wantErr: false,
			desc:    "Month step with base at max (12) should pass",
		},
		{
			name:    "month_step_base_above_max",
			expr:    "0 0 1 13/2 *",
			wantErr: true,
			desc:    "Month step with base above max (13 for months) should fail",
		},
		// Day of week field steps
		{
			name:    "dow_step_base_at_min",
			expr:    "0 0 * * 0/2",
			wantErr: false,
			desc:    "Day of week step with base at min (0) should pass",
		},
		{
			name:    "dow_step_base_at_max",
			expr:    "0 0 * * 6/2",
			wantErr: false,
			desc:    "Day of week step with base at max (6) should pass",
		},
		{
			name:    "dow_step_base_above_max",
			expr:    "0 0 * * 8/2",
			wantErr: true,
			desc:    "Day of week step with base above max (8, max is 7) should fail",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := s.ValidateCronExpression(tc.expr)
			if (err != nil) != tc.wantErr {
				t.Errorf("%s: got error %v, wantErr %v", tc.desc, err, tc.wantErr)
			}
		})
	}
}

func TestValidateJobName_BoundaryConditions(t *testing.T) {
	t.Parallel()
	s := NewSanitizer()

	testCases := []struct {
		name    string
		jobName string
		wantErr bool
		desc    string
	}{
		// Targeting line 378: if len(name) == 0 || len(name) > 100
		{
			name:    "empty_name",
			jobName: "",
			wantErr: true,
			desc:    "Empty job name should fail",
		},
		{
			name:    "single_char_name",
			jobName: "a",
			wantErr: false,
			desc:    "Single char job name should pass",
		},
		{
			name:    "name_exactly_100",
			jobName: strings.Repeat("a", 100),
			wantErr: false,
			desc:    "Job name exactly 100 chars should pass",
		},
		{
			name:    "name_at_99",
			jobName: strings.Repeat("a", 99),
			wantErr: false,
			desc:    "Job name at 99 chars should pass",
		},
		{
			name:    "name_at_101",
			jobName: strings.Repeat("a", 101),
			wantErr: true,
			desc:    "Job name at 101 chars should fail",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := s.ValidateJobName(tc.jobName)
			if (err != nil) != tc.wantErr {
				t.Errorf("%s: got error %v, wantErr %v", tc.desc, err, tc.wantErr)
			}
		})
	}
}
