package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestValidatorBoundaryConditions tests boundary conditions that mutation testing identified
func TestValidatorBoundaryConditions(t *testing.T) {
	t.Parallel()

	t.Run("ValidateMinLength boundary", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name      string
			value     string
			minLen    int
			wantError bool
		}{
			{"exactly at min", "abc", 3, false},
			{"one below min", "ab", 3, true},
			{"one above min", "abcd", 3, false},
			{"empty with min 0", "", 0, false},
			{"empty with min 1", "", 1, true},
			{"min length 0", "x", 0, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				v := NewValidator()
				v.ValidateMinLength("field", tt.value, tt.minLen)
				if v.HasErrors() != tt.wantError {
					t.Errorf("ValidateMinLength(%q, %d) hasError = %v, want %v",
						tt.value, tt.minLen, v.HasErrors(), tt.wantError)
				}
			})
		}
	})

	t.Run("ValidateMaxLength boundary", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name      string
			value     string
			maxLen    int
			wantError bool
		}{
			{"exactly at max", "abc", 3, false},
			{"one below max", "ab", 3, false},
			{"one above max", "abcd", 3, true},
			{"empty with max 0", "", 0, false},
			{"empty with max 5", "", 5, false},
			{"max length 0 with content", "x", 0, true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				v := NewValidator()
				v.ValidateMaxLength("field", tt.value, tt.maxLen)
				if v.HasErrors() != tt.wantError {
					t.Errorf("ValidateMaxLength(%q, %d) hasError = %v, want %v",
						tt.value, tt.maxLen, v.HasErrors(), tt.wantError)
				}
			})
		}
	})

	t.Run("ValidateRange boundary", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name      string
			value     int
			min, max  int
			wantError bool
		}{
			{"at min", 10, 10, 20, false},
			{"at max", 20, 10, 20, false},
			{"one below min", 9, 10, 20, true},
			{"one above max", 21, 10, 20, true},
			{"middle of range", 15, 10, 20, false},
			{"negative range", -5, -10, 0, false},
			{"negative below", -11, -10, 0, true},
			{"zero range", 0, 0, 0, false},
			{"zero below range", -1, 0, 0, true},
			{"zero above range", 1, 0, 0, true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				v := NewValidator()
				v.ValidateRange("field", tt.value, tt.min, tt.max)
				if v.HasErrors() != tt.wantError {
					t.Errorf("ValidateRange(%d, %d, %d) hasError = %v, want %v",
						tt.value, tt.min, tt.max, v.HasErrors(), tt.wantError)
				}
			})
		}
	})

	t.Run("ValidatePositive boundary", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name      string
			value     int
			wantError bool
		}{
			{"zero", 0, true},
			{"one", 1, false},
			{"negative one", -1, true},
			{"large positive", 1000000, false},
			{"large negative", -1000000, true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				v := NewValidator()
				v.ValidatePositive("field", tt.value)
				if v.HasErrors() != tt.wantError {
					t.Errorf("ValidatePositive(%d) hasError = %v, want %v",
						tt.value, v.HasErrors(), tt.wantError)
				}
			})
		}
	})
}

// TestValidatePathBoundary tests ValidatePath edge cases (line 189 mutation)
func TestValidatePathBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{"empty path", "", false}, // Empty is allowed
		{"valid path", "/var/log", false},
		{"null byte", "/var/\x00log", true},
		{"relative path", "relative/path", false},
		{"path with spaces", "/path with spaces/file", false},
		{"just slash", "/", false},
		{"dot path", ".", false},
		{"double dot", "..", false},
		{"windows-like path", "C:\\Users\\test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.ValidatePath("path", tt.value)
			if v.HasErrors() != tt.wantError {
				t.Errorf("ValidatePath(%q) hasError = %v, want %v",
					tt.value, v.HasErrors(), tt.wantError)
			}
		})
	}
}

// TestValidateCronExpressionBoundary tests cron validation edge cases
func TestValidateCronExpressionBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cron      string
		wantError bool
	}{
		// Empty - allowed
		{"empty", "", false},

		// Special expressions - exact boundary
		{"@yearly", "@yearly", false},
		{"@annually", "@annually", false},
		{"@monthly", "@monthly", false},
		{"@weekly", "@weekly", false},
		{"@daily", "@daily", false},
		{"@midnight", "@midnight", false},
		{"@hourly", "@hourly", false},
		{"@every with duration", "@every 5m", false},
		{"@triggered", "@triggered", false},
		{"@manual", "@manual", false},
		{"@none", "@none", false},

		// Invalid special expressions
		{"@invalid", "@invalid", true},
		{"@yearlyX", "@yearlyX", true},
		{"@every no duration", "@every", true}, // go-cron requires a duration after @every

		// Field count boundaries
		{"4 fields", "* * * *", true},
		{"5 fields", "* * * * *", false},
		{"6 fields", "* * * * * *", false},
		{"7 fields", "* * * * * * *", true},

		// Character validation
		{"question mark", "0 0 ? * *", false},
		{"hash invalid", "0 0 # * *", true},
		{"letters", "a b c d e", true},
		{"mixed valid", "0,15,30,45 * * * *", false},
		{"ranges", "0-30 * * * *", false},
		{"steps", "*/5 * * * *", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.ValidateCronExpression("cron", tt.cron)
			if v.HasErrors() != tt.wantError {
				t.Errorf("ValidateCronExpression(%q) hasError = %v, want %v",
					tt.cron, v.HasErrors(), tt.wantError)
			}
		})
	}
}

// TestValidator2IsValidAddress tests isValidAddress edge cases (lines 464-483)
func TestValidator2IsValidAddress(t *testing.T) {
	t.Parallel()

	cv := &Validator2{sanitizer: NewSanitizer()}

	tests := []struct {
		name  string
		addr  string
		valid bool
	}{
		// Empty - invalid
		{"empty string", "", false},

		// Valid formats
		{"localhost:8080", "localhost:8080", true},
		{"127.0.0.1:8080", "127.0.0.1:8080", true},
		{":8080 (any interface)", ":8080", true},
		{"0.0.0.0:80", "0.0.0.0:80", true},
		{"hostname:443", "hostname:443", true},

		// Invalid - no colon
		{"no colon", "localhost8080", false},
		{"just host", "localhost", false},

		// Invalid - multiple colons
		{"multiple colons", "local:host:8080", false},
		{"IPv6 without bracket", "::1:8080", false},

		// Invalid port
		{"non-numeric port", "localhost:abc", false},
		{"empty port", "localhost:", false},

		// Edge cases
		{"port only numeric check", ":12345", true},
		{"high port", ":65535", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := cv.isValidAddress(tt.addr)
			if got != tt.valid {
				t.Errorf("isValidAddress(%q) = %v, want %v", tt.addr, got, tt.valid)
			}
		})
	}
}

// TestValidator2IsValidLogLevel tests isValidLogLevel edge cases (lines 486-495)
func TestValidator2IsValidLogLevel(t *testing.T) {
	t.Parallel()

	cv := &Validator2{sanitizer: NewSanitizer()}

	tests := []struct {
		name  string
		level string
		valid bool
	}{
		// Valid levels (lowercase)
		{"debug", "debug", true},
		{"info", "info", true},
		{"notice", "notice", true},
		{"warning", "warning", true},
		{"error", "error", true},
		{"critical", "critical", true},

		// Valid levels (uppercase - should be normalized)
		{"DEBUG uppercase", "DEBUG", true},
		{"INFO uppercase", "INFO", true},
		{"Warning mixed", "Warning", true},

		// Valid levels (aliases accepted by ApplyLogLevel)
		{"trace", "trace", true},
		{"fatal", "fatal", true},
		{"warn", "warn", true},
		{"panic", "panic", true},

		// Invalid levels
		{"empty", "", false},
		{"random", "random", false},
		{"verbose", "verbose", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := cv.isValidLogLevel(tt.level)
			if got != tt.valid {
				t.Errorf("isValidLogLevel(%q) = %v, want %v", tt.level, got, tt.valid)
			}
		})
	}
}

// TestValidator2ValidateIntField tests validateIntField port validation
func TestValidator2ValidateIntField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		value     int64
		wantError bool
	}{
		// Port validation
		{"valid port", "server-port", 8080, false},
		{"port at min", "port", 1, false},
		{"port at max", "http-port", 65535, false},
		{"port zero", "api_port", 0, false}, // 0 is not validated as port when value is 0
		{"port too high", "smtp-port", 65536, true},
		{"port negative", "ssh_port", -1, false}, // Negative ports bypass validation (val > 0 check)

		// Max/size validation
		{"max positive", "max-connections", 100, false},
		{"max zero", "max-workers", 0, false},
		{"max negative", "max-size", -1, true},
		{"size negative", "buffer-size", -10, true},

		// Non-port/max fields
		{"other field", "timeout", 30, false},
		{"other negative", "other-field", -5, false}, // No validation for non-port/max
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cv := &Validator2{sanitizer: NewSanitizer()}
			v := NewValidator()

			field := reflect.ValueOf(tt.value)
			cv.validateIntField(v, field, tt.path)

			if v.HasErrors() != tt.wantError {
				t.Errorf("validateIntField(%q, %d) hasError = %v, want %v",
					tt.path, tt.value, v.HasErrors(), tt.wantError)
			}
		})
	}
}

// TestValidator2ValidateStringFieldDefaults tests string field with defaults
func TestValidator2ValidateStringFieldDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		value      string
		defaultTag string
		path       string
		wantError  bool
	}{
		// With default tag - empty allowed
		{"empty with default", "", "default-value", "field", false},
		{"non-empty with default", "value", "default-value", "field", false},

		// Without default tag - empty may be required
		{"empty optional field", "", "", "smtp-user", false}, // Optional field
		{"empty optional email", "", "", "email-to", false},  // Optional field
		{"non-empty no default", "value", "", "some-field", false},

		// Special fields
		{"log-level with default", "", "info", "log-level", false},
		{"schedule empty", "", "", "schedule", true}, // schedule is required when no default
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cv := &Validator2{sanitizer: NewSanitizer()}
			v := NewValidator()

			field := reflect.ValueOf(tt.value)
			cv.validateStringField(v, field, tt.path, tt.defaultTag)

			if v.HasErrors() != tt.wantError {
				t.Errorf("validateStringField(%q, %q, %q) hasError = %v, want %v",
					tt.value, tt.path, tt.defaultTag, v.HasErrors(), tt.wantError)
			}
		})
	}
}

// TestValidator2IsOptionalField tests isOptionalField boundary
func TestValidator2IsOptionalField(t *testing.T) {
	t.Parallel()

	cv := &Validator2{sanitizer: NewSanitizer()}

	optionalFields := []string{
		"smtp-user", "smtp-password", "email-to", "email-from",
		"slack-webhook", "slack-channel", "save-folder",
		"container", "service", "image", "user", "network",
		"environment", "secrets", "volumes", "working_dir",
		"log-level",
	}

	for _, field := range optionalFields {
		t.Run("optional: "+field, func(t *testing.T) {
			t.Parallel()
			if !cv.isOptionalField(field) {
				t.Errorf("isOptionalField(%q) = false, want true", field)
			}
		})
	}

	requiredFields := []string{
		"schedule", "command", "name", "required-field",
	}

	for _, field := range requiredFields {
		t.Run("required: "+field, func(t *testing.T) {
			t.Parallel()
			if cv.isOptionalField(field) {
				t.Errorf("isOptionalField(%q) = true, want false", field)
			}
		})
	}
}

// TestValidator2ValidateStruct tests validateStruct with various struct types
func TestValidator2ValidateStruct(t *testing.T) {
	t.Parallel()

	t.Run("nil pointer", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()

		var nilPtr *struct{ Name string }
		cv.validateStruct(v, nilPtr, "")

		// Should not panic and have no errors
		if v.HasErrors() {
			t.Error("nil pointer should not produce errors")
		}
	})

	t.Run("non-struct type", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()

		cv.validateStruct(v, "string value", "")

		// Should not panic and have no errors
		if v.HasErrors() {
			t.Error("non-struct should not produce errors")
		}
	})

	t.Run("struct with gcfg tag", func(t *testing.T) {
		t.Parallel()
		type TestStruct struct {
			FieldName string `gcfg:"custom-name"`
		}

		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()

		obj := &TestStruct{FieldName: "value"}
		cv.validateStruct(v, obj, "")

		// Field should be validated with gcfg tag name
		// No errors expected for valid value
	})

	t.Run("struct with mapstructure tag", func(t *testing.T) {
		t.Parallel()
		type TestStruct struct {
			FieldName string `mapstructure:"mapped-name"`
		}

		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()

		obj := &TestStruct{FieldName: "value"}
		cv.validateStruct(v, obj, "")

		// Field should be validated with mapstructure tag name
	})

	t.Run("struct with squash tag", func(t *testing.T) {
		t.Parallel()
		type Embedded struct {
			Inner string
		}
		type TestStruct struct {
			Embedded `mapstructure:",squash"`
		}

		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()

		obj := &TestStruct{Embedded: Embedded{Inner: "value"}}
		cv.validateStruct(v, obj, "")

		// Squashed struct should be handled differently
	})

	t.Run("nested struct path building", func(t *testing.T) {
		t.Parallel()
		type Inner struct {
			Value string `gcfg:"value"`
		}
		type Outer struct {
			Inner Inner `gcfg:"inner"`
		}

		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()

		obj := &Outer{Inner: Inner{Value: "test"}}
		cv.validateStruct(v, obj, "root")

		// Path should be built correctly for nested structs
	})
}

// TestValidatorErrorMessages tests error message formatting
func TestValidatorErrorMessages(t *testing.T) {
	t.Parallel()

	t.Run("ValidationError string format", func(t *testing.T) {
		t.Parallel()
		err := ValidationError{
			Field:   "test-field",
			Value:   "test-value",
			Message: "must be valid",
		}

		errStr := err.Error()
		if !strings.Contains(errStr, "test-field") {
			t.Error("Error should contain field name")
		}
		if !strings.Contains(errStr, "test-value") {
			t.Error("Error should contain value")
		}
		if !strings.Contains(errStr, "must be valid") {
			t.Error("Error should contain message")
		}
	})

	t.Run("ValidationErrors combined", func(t *testing.T) {
		t.Parallel()
		errors := ValidationErrors{
			{Field: "field1", Value: "v1", Message: "error1"},
			{Field: "field2", Value: "v2", Message: "error2"},
			{Field: "field3", Value: "v3", Message: "error3"},
		}

		errStr := errors.Error()
		if !strings.Contains(errStr, "field1") {
			t.Error("Combined error should contain field1")
		}
		if !strings.Contains(errStr, "field2") {
			t.Error("Combined error should contain field2")
		}
		if !strings.Contains(errStr, "field3") {
			t.Error("Combined error should contain field3")
		}
	})

	t.Run("empty ValidationErrors", func(t *testing.T) {
		t.Parallel()
		errors := ValidationErrors{}
		errStr := errors.Error()
		if errStr != "" {
			t.Errorf("Empty ValidationErrors should produce empty string, got %q", errStr)
		}
	})
}

// TestValidateEnumBoundary tests ValidateEnum edge cases
func TestValidateEnumBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		allowed   []string
		wantError bool
	}{
		{"empty value allowed", "", []string{"a", "b"}, false},
		{"first option", "a", []string{"a", "b", "c"}, false},
		{"last option", "c", []string{"a", "b", "c"}, false},
		{"middle option", "b", []string{"a", "b", "c"}, false},
		{"not in list", "d", []string{"a", "b", "c"}, true},
		{"case sensitive", "A", []string{"a", "b", "c"}, true},
		{"single option valid", "only", []string{"only"}, false},
		{"single option invalid", "other", []string{"only"}, true},
		{"empty allowed list", "any", []string{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.ValidateEnum("field", tt.value, tt.allowed)
			if v.HasErrors() != tt.wantError {
				t.Errorf("ValidateEnum(%q, %v) hasError = %v, want %v",
					tt.value, tt.allowed, v.HasErrors(), tt.wantError)
			}
		})
	}
}

// TestValidateURLBoundary tests ValidateURL edge cases
func TestValidateURLBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		url       string
		wantError bool
	}{
		{"empty", "", false},
		{"http valid", "http://example.com", false},
		{"https valid", "https://example.com", false},
		{"ftp valid", "ftp://files.example.com", false},
		{"with path", "https://example.com/path/to/resource", false},
		{"with query", "https://example.com?query=value", false},
		{"with port", "https://example.com:8080", false},
		{"no scheme", "example.com", true},             // Scheme required
		{"no host", "http://", true},                   // Host required
		{"just path", "/path/only", true},              // Missing scheme and host
		{"double slash", "//example.com", true},        // Missing scheme
		{"invalid chars", "http://exam ple.com", true}, // Parse error due to space
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.ValidateURL("url", tt.url)
			if v.HasErrors() != tt.wantError {
				t.Errorf("ValidateURL(%q) hasError = %v, want %v",
					tt.url, v.HasErrors(), tt.wantError)
			}
		})
	}
}

// TestValidateEmailBoundary tests ValidateEmail edge cases
func TestValidateEmailBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		email     string
		wantError bool
	}{
		{"empty", "", false},
		{"simple valid", "user@example.com", false},
		{"with dots", "user.name@example.com", false},
		{"with plus", "user+tag@example.com", false},
		{"with subdomain", "user@mail.example.com", false},
		{"short tld", "user@example.co", false},
		{"long tld", "user@example.museum", false},
		{"no at sign", "userexample.com", true},
		{"no domain", "user@", true},
		{"no user", "@example.com", true},
		{"double at", "user@@example.com", true},
		{"space in email", "user @example.com", true},
		{"no tld", "user@example", true},
		{"dot at end", "user@example.", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.ValidateEmail("email", tt.email)
			if v.HasErrors() != tt.wantError {
				t.Errorf("ValidateEmail(%q) hasError = %v, want %v",
					tt.email, v.HasErrors(), tt.wantError)
			}
		})
	}
}
