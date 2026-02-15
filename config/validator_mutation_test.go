package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// ============================================================================
// Tests targeting 15 survived CONDITIONALS_NEGATION mutations in config/validator.go
//
// All mutations are CONDITIONALS_NEGATION, meaning the mutant negates a boolean
// condition. To kill such a mutant, we need a test that:
// 1. Exercises the exact condition
// 2. Asserts a different outcome depending on whether the condition is true or false
//
// Lines targeted: 129, 132, 135, 138, 141, 154, 157, 159, 163, 166, 172, 175, 178, 180, 183
// These fall within ValidateCronExpression, ValidateEnum, ValidatePath, and
// the Validator2/NewConfigValidator area.
// ============================================================================

// --- ValidateCronExpression mutations (lines ~129-141) ---
// These negations target:
// - `if value == ""`  -> `if value != ""`
// - `if value == "@triggered" || ...` -> negating individual comparisons
// - `if err := cron.ValidateSpec(value, parseOpts); err != nil` -> `err == nil`

// TestValidateCronExpression_EmptyVsNonEmpty kills the CONDITIONALS_NEGATION
// on `if value == ""`. When value IS empty, we must return without error.
// When value is NOT empty, we must validate it.
func TestValidateCronExpression_EmptyVsNonEmpty(t *testing.T) {
	t.Parallel()

	// Empty value -> no error (early return)
	v := NewValidator()
	v.ValidateCronExpression("schedule", "")
	if v.HasErrors() {
		t.Error("ValidateCronExpression with empty string should NOT produce an error")
	}

	// Non-empty invalid value -> error
	v2 := NewValidator()
	v2.ValidateCronExpression("schedule", "not-a-cron")
	if !v2.HasErrors() {
		t.Error("ValidateCronExpression with invalid non-empty string SHOULD produce an error")
	}

	// Non-empty valid value -> no error
	v3 := NewValidator()
	v3.ValidateCronExpression("schedule", "* * * * *")
	if v3.HasErrors() {
		t.Error("ValidateCronExpression with valid cron should NOT produce an error")
	}
}

// TestValidateCronExpression_TriggeredKeywords kills CONDITIONALS_NEGATION on
// `value == "@triggered"`, `value == "@manual"`, `value == "@none"`.
// Each keyword must return early (no error). If negated, they would be treated
// as invalid cron expressions.
func TestValidateCronExpression_TriggeredKeywords(t *testing.T) {
	t.Parallel()

	keywords := []string{"@triggered", "@manual", "@none"}
	for _, kw := range keywords {
		t.Run(kw, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.ValidateCronExpression("schedule", kw)
			if v.HasErrors() {
				t.Errorf("ValidateCronExpression(%q) should NOT produce an error (triggered keyword)", kw)
			}
		})
	}

	// Confirm that similar but different values ARE validated
	nonKeywords := []string{"@trigger", "@manu", "@no", "triggered", "manual", "none"}
	for _, nk := range nonKeywords {
		t.Run("invalid_"+nk, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.ValidateCronExpression("schedule", nk)
			if !v.HasErrors() {
				t.Errorf("ValidateCronExpression(%q) SHOULD produce an error (not a valid keyword)", nk)
			}
		})
	}
}

// TestValidateCronExpression_ParseError kills CONDITIONALS_NEGATION on
// `if err := cron.ValidateSpec(value, parseOpts); err != nil`.
// If negated to `err == nil`, valid expressions would produce errors
// and invalid expressions would pass.
func TestValidateCronExpression_ParseError(t *testing.T) {
	t.Parallel()

	// Valid expression -> no error (err == nil, so NOT entered)
	v := NewValidator()
	v.ValidateCronExpression("schedule", "0 0 * * *")
	if v.HasErrors() {
		t.Error("Valid cron expression should NOT produce an error")
	}

	// Invalid expression -> error (err != nil, so entered)
	v2 := NewValidator()
	v2.ValidateCronExpression("schedule", "invalid cron")
	if !v2.HasErrors() {
		t.Error("Invalid cron expression SHOULD produce an error")
	}
}

// TestValidateCronExpression_ParseOptsFlags kills CONDITIONALS_NEGATION on the
// parseOpts bitwise-OR line. The options define valid parser flags.
func TestValidateCronExpression_WithSeconds(t *testing.T) {
	t.Parallel()

	// 6-field (with optional seconds) should be valid
	v := NewValidator()
	v.ValidateCronExpression("schedule", "30 0 0 * * *")
	if v.HasErrors() {
		t.Error("6-field cron expression (with seconds) should be valid")
	}

	// Standard 5-field should be valid
	v2 := NewValidator()
	v2.ValidateCronExpression("schedule", "0 0 * * *")
	if v2.HasErrors() {
		t.Error("5-field cron expression should be valid")
	}
}

// --- ValidateEnum mutations (lines ~149-157) ---
// Negations on:
// - `if value == ""` -> `if value != ""`
// - `if slices.Contains(allowed, value)` -> `if !slices.Contains(allowed, value)`

func TestValidateEnum_EmptyVsNonEmpty(t *testing.T) {
	t.Parallel()

	allowed := []string{"a", "b", "c"}

	// Empty value -> no error (early return)
	v := NewValidator()
	v.ValidateEnum("field", "", allowed)
	if v.HasErrors() {
		t.Error("ValidateEnum with empty value should NOT produce an error")
	}

	// Valid non-empty value -> no error
	v2 := NewValidator()
	v2.ValidateEnum("field", "a", allowed)
	if v2.HasErrors() {
		t.Error("ValidateEnum with allowed value should NOT produce an error")
	}

	// Invalid non-empty value -> error
	v3 := NewValidator()
	v3.ValidateEnum("field", "d", allowed)
	if !v3.HasErrors() {
		t.Error("ValidateEnum with non-allowed value SHOULD produce an error")
	}
}

// TestValidateEnum_ContainsNegation specifically tests the slices.Contains negation.
// If `slices.Contains` is negated, allowed values would produce errors
// and disallowed values would pass.
func TestValidateEnum_ContainsNegation(t *testing.T) {
	t.Parallel()

	allowed := []string{"debug", "info", "warning", "error"}

	tests := []struct {
		value     string
		wantError bool
	}{
		{"debug", false},   // In list -> no error
		{"info", false},    // In list -> no error
		{"warning", false}, // In list -> no error
		{"error", false},   // In list -> no error
		{"trace", true},    // NOT in list -> error
		{"fatal", true},    // NOT in list -> error
		{"DEBUG", true},    // Case sensitive, NOT in list -> error
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.ValidateEnum("level", tt.value, allowed)
			if v.HasErrors() != tt.wantError {
				t.Errorf("ValidateEnum(%q) hasErrors=%v, want %v", tt.value, v.HasErrors(), tt.wantError)
			}
		})
	}
}

// --- ValidatePath mutations (lines ~159-167) ---
// Negations on:
// - `if value == ""` -> `if value != ""`
// - `if strings.ContainsAny(value, "\x00")` -> `if !strings.ContainsAny(value, "\x00")`

func TestValidatePath_EmptyVsNonEmpty(t *testing.T) {
	t.Parallel()

	// Empty value -> no error (early return)
	v := NewValidator()
	v.ValidatePath("path", "")
	if v.HasErrors() {
		t.Error("ValidatePath with empty value should NOT produce an error")
	}

	// Non-empty valid path -> no error
	v2 := NewValidator()
	v2.ValidatePath("path", "/var/log")
	if v2.HasErrors() {
		t.Error("ValidatePath with valid path should NOT produce an error")
	}

	// Non-empty path with null byte -> error
	v3 := NewValidator()
	v3.ValidatePath("path", "/var/\x00log")
	if !v3.HasErrors() {
		t.Error("ValidatePath with null byte SHOULD produce an error")
	}
}

// TestValidatePath_ContainsAnyNegation specifically tests the ContainsAny negation.
// If negated, valid paths would get errors and paths with null bytes would pass.
func TestValidatePath_ContainsAnyNegation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{"valid_path", "/usr/local/bin", false},
		{"null_byte_start", "\x00/path", true},
		{"null_byte_middle", "/pa\x00th", true},
		{"null_byte_end", "/path\x00", true},
		{"no_null_byte", "/normal/path/file.txt", false},
		{"spaces_ok", "/path with spaces", false},
		{"special_chars_ok", "/path/@#%/file", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.ValidatePath("path", tt.value)
			if v.HasErrors() != tt.wantError {
				t.Errorf("ValidatePath(%q) hasErrors=%v, want %v", tt.value, v.HasErrors(), tt.wantError)
			}
		})
	}
}

// --- Validator2 / NewConfigValidator mutations (lines ~172-183) ---
// These target the struct definition and constructor. The mutations might be on
// conditions that check if config or sanitizer fields are properly initialized.

// TestNewConfigValidator_SanitizerInitialized verifies that NewConfigValidator
// always initializes the sanitizer. If a CONDITIONALS_NEGATION mutant somehow
// skips initialization, the sanitizer would be nil.
func TestNewConfigValidator_SanitizerInitialized(t *testing.T) {
	t.Parallel()

	type SimpleConfig struct {
		Name string `mapstructure:"name"`
	}

	cv := NewConfigValidator(SimpleConfig{Name: "test"})
	if cv.sanitizer == nil {
		t.Fatal("NewConfigValidator should always initialize the sanitizer")
	}
	if cv.config == nil {
		t.Fatal("NewConfigValidator should store the config")
	}
}

// TestNewConfigValidator_ConfigStored verifies the config is stored.
func TestNewConfigValidator_ConfigStored(t *testing.T) {
	t.Parallel()

	config := struct {
		Value string `mapstructure:"value"`
	}{Value: "hello"}

	cv := NewConfigValidator(config)
	if cv.config == nil {
		t.Fatal("Config should be stored")
	}
}

// --- Combined mutation tests that exercise multiple conditions in a single flow ---

// TestValidator2_FullValidationFlow runs a complete validation that exercises
// all the ValidateCronExpression, ValidateEnum, and ValidatePath code paths
// through the Validator2's validateSpecificStringField dispatch.
func TestValidator2_FullValidationFlow_Schedule(t *testing.T) {
	t.Parallel()

	type ScheduleConfig struct {
		Schedule string `mapstructure:"schedule"`
	}

	// Valid schedule
	cv := NewConfigValidator(ScheduleConfig{Schedule: "* * * * *"})
	err := cv.Validate()
	if err != nil {
		t.Errorf("Valid schedule should not produce error: %v", err)
	}

	// Invalid schedule
	cv2 := NewConfigValidator(ScheduleConfig{Schedule: "invalid"})
	err2 := cv2.Validate()
	if err2 == nil {
		t.Error("Invalid schedule should produce error")
	}

	// Triggered schedule
	cv3 := NewConfigValidator(ScheduleConfig{Schedule: "@triggered"})
	err3 := cv3.Validate()
	if err3 != nil {
		t.Errorf("@triggered schedule should be valid: %v", err3)
	}

	// Manual schedule
	cv4 := NewConfigValidator(ScheduleConfig{Schedule: "@manual"})
	err4 := cv4.Validate()
	if err4 != nil {
		t.Errorf("@manual schedule should be valid: %v", err4)
	}

	// None schedule
	cv5 := NewConfigValidator(ScheduleConfig{Schedule: "@none"})
	err5 := cv5.Validate()
	if err5 != nil {
		t.Errorf("@none schedule should be valid: %v", err5)
	}

	// Empty schedule - should trigger required check
	cv6 := NewConfigValidator(ScheduleConfig{Schedule: ""})
	err6 := cv6.Validate()
	if err6 == nil {
		t.Error("Empty schedule should produce error (required field)")
	}
}

// TestValidator2_FullValidationFlow_Path tests path validation through Validator2.
func TestValidator2_FullValidationFlow_Path(t *testing.T) {
	t.Parallel()

	type PathConfig struct {
		SaveFolder string `mapstructure:"save-folder"`
	}

	// Valid path
	cv := NewConfigValidator(PathConfig{SaveFolder: "/var/log/ofelia"})
	err := cv.Validate()
	if err != nil {
		t.Errorf("Valid save-folder should not produce error: %v", err)
	}

	// Path with control characters should fail (sanitizer rejects control chars)
	cv2 := NewConfigValidator(PathConfig{SaveFolder: "/var/\x01log"})
	err2 := cv2.Validate()
	if err2 == nil {
		t.Error("Save-folder with control character should produce error")
	}

	// Empty path (optional field) should pass
	cv3 := NewConfigValidator(PathConfig{SaveFolder: ""})
	err3 := cv3.Validate()
	if err3 != nil {
		t.Errorf("Empty save-folder (optional) should not produce error: %v", err3)
	}
}

// --- Tests that verify validateIntField conditions ---
// These may correspond to mutations at lines around 172-183 depending on
// the exact line mapping.

func TestValidateIntField_PortBoundary(t *testing.T) {
	t.Parallel()

	cv := &Validator2{sanitizer: NewSanitizer()}

	tests := []struct {
		name      string
		path      string
		value     int64
		wantError bool
	}{
		// Port validation: val > 0 condition
		{"port_at_1", "port", 1, false},
		{"port_at_65535", "port", 65535, false},
		{"port_at_0_skip", "port", 0, false},   // val > 0 is false, so no port validation
		{"port_at_65536", "port", 65536, true}, // Out of range
		{"port_negative", "port", -1, false},   // val > 0 is false, skip
		// Max/size validation: val < 0 condition
		{"max_positive", "max-workers", 10, false},
		{"max_zero", "max-items", 0, false},
		{"max_negative", "max-size", -1, true},     // val < 0
		{"size_negative", "buffer-size", -5, true}, // val < 0
		{"size_positive", "buffer-size", 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			field := reflect.ValueOf(tt.value)
			cv.validateIntField(v, field, tt.path)
			if v.HasErrors() != tt.wantError {
				t.Errorf("validateIntField(%q, %d) hasErrors=%v, want %v",
					tt.path, tt.value, v.HasErrors(), tt.wantError)
			}
		})
	}
}

// TestValidateStringField_DefaultTagConditions tests the string field validation
// with default tag conditions.
func TestValidateStringField_DefaultTagConditions(t *testing.T) {
	t.Parallel()

	cv := &Validator2{sanitizer: NewSanitizer()}

	tests := []struct {
		name       string
		value      string
		defaultTag string
		path       string
		wantError  bool
	}{
		// defaultTag != "" && str == "" -> early return (no error)
		{"empty_with_default", "", "mydefault", "field", false},
		// defaultTag != "" && str != "" -> validate normally
		{"non_empty_with_default", "value", "mydefault", "field", false},
		// defaultTag == "" && str == "" && optional -> no required error
		{"empty_optional", "", "", "smtp-user", false},
		// defaultTag == "" && str == "" && NOT optional -> required error
		{"empty_required", "", "", "schedule", true},
		// defaultTag == "" && str != "" -> validate specific field
		{"non_empty_required", "* * * * *", "", "schedule", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			field := reflect.ValueOf(tt.value)
			cv.validateStringField(v, field, tt.path, tt.defaultTag)
			if v.HasErrors() != tt.wantError {
				t.Errorf("validateStringField(%q, %q, %q) hasErrors=%v, want %v",
					tt.value, tt.path, tt.defaultTag, v.HasErrors(), tt.wantError)
			}
		})
	}
}

// TestPerformSecurityValidation_SanitizerNilVsNonNil tests the sanitizer nil check.
func TestPerformSecurityValidation_SanitizerNilVsNonNil(t *testing.T) {
	t.Parallel()

	// With sanitizer - control characters (other than \t\n\r and \x00) should fail
	// Note: \x00 is stripped (not rejected) by SanitizeString, but \x01 is a control char that IS rejected
	t.Run("with_sanitizer_bad_input", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		result := cv.performSecurityValidation(v, "field", "test\x01value")
		if result {
			t.Error("Security validation should reject control characters when sanitizer is present")
		}
		if !v.HasErrors() {
			t.Error("Should have validation errors for control characters")
		}
	})

	// With sanitizer - input exceeding max length should fail
	t.Run("with_sanitizer_too_long", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		longInput := make([]byte, 1025)
		for i := range longInput {
			longInput[i] = 'a'
		}
		result := cv.performSecurityValidation(v, "field", string(longInput))
		if result {
			t.Error("Security validation should reject input exceeding 1024 chars")
		}
		if !v.HasErrors() {
			t.Error("Should have validation errors for too-long input")
		}
	})

	// With sanitizer - good input should pass
	t.Run("with_sanitizer_good_input", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		result := cv.performSecurityValidation(v, "field", "normal-value")
		if !result {
			t.Error("Security validation should accept normal input")
		}
		if v.HasErrors() {
			t.Errorf("Should have no errors for normal input: %v", v.Errors())
		}
	})

	// Without sanitizer - should return true (no validation)
	t.Run("without_sanitizer", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: nil}
		v := NewValidator()
		result := cv.performSecurityValidation(v, "field", "any\x01value")
		if !result {
			t.Error("Security validation should return true when sanitizer is nil (skip validation)")
		}
		if v.HasErrors() {
			t.Error("Should have no errors when sanitizer is nil")
		}
	})
}

// TestValidateSpecificStringField_AllPaths tests all the switch cases in
// validateSpecificStringField to ensure each path is exercised.
func TestValidateSpecificStringField_AllPaths(t *testing.T) {
	t.Parallel()

	cv := &Validator2{sanitizer: NewSanitizer()}

	// Each path name triggers different validation logic
	paths := []struct {
		path        string
		validInput  string
		invalidDesc string
	}{
		{"schedule", "* * * * *", "cron"},
		{"cron", "@daily", "cron alias"},
		{"email-to", "user@example.com", "email"},
		{"email-from", "sender@example.com", "email"},
		{"web-address", ":8080", "address"},
		{"pprof-address", "localhost:6060", "address"},
		{"log-level", "debug", "log level"},
		{"command", "echo hello", "command"},
		{"cmd", "ls -la", "command alias"},
		{"image", "nginx:latest", "docker image"},
		{"save-folder", "/var/log", "path"},
		{"working_dir", "/app", "path alias"},
	}

	for _, p := range paths {
		t.Run(fmt.Sprintf("valid_%s", p.path), func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			cv.validateSpecificStringField(v, p.path, p.validInput)
			// Valid input should not produce errors
			if v.HasErrors() {
				t.Errorf("validateSpecificStringField(%q, %q) should not error: %v",
					p.path, p.validInput, v.Errors())
			}
		})
	}
}

// TestValidateAddressField_BothBranches tests both valid and invalid address inputs.
func TestValidateAddressField_BothBranches(t *testing.T) {
	t.Parallel()

	cv := &Validator2{sanitizer: NewSanitizer()}

	// Valid address -> no error
	v := NewValidator()
	cv.validateAddressField(v, "web-address", ":8080")
	if v.HasErrors() {
		t.Error("Valid address should not produce error")
	}

	// Invalid address -> error
	v2 := NewValidator()
	cv.validateAddressField(v2, "web-address", "no-port")
	if !v2.HasErrors() {
		t.Error("Invalid address should produce error")
	}
}

// TestValidateLogLevelField_BothBranches tests both valid and invalid log levels.
func TestValidateLogLevelField_BothBranches(t *testing.T) {
	t.Parallel()

	cv := &Validator2{sanitizer: NewSanitizer()}

	// Valid level -> no error
	v := NewValidator()
	cv.validateLogLevelField(v, "log-level", "info")
	if v.HasErrors() {
		t.Error("Valid log level should not produce error")
	}

	// Invalid level -> error
	v2 := NewValidator()
	cv.validateLogLevelField(v2, "log-level", "invalid")
	if !v2.HasErrors() {
		t.Error("Invalid log level should produce error")
	}
}

// TestIsOptionalField_BothBranches exercises the isOptionalField method.
func TestIsOptionalField_BothBranches(t *testing.T) {
	t.Parallel()

	cv := &Validator2{sanitizer: NewSanitizer()}

	// Optional fields should return true
	optionalFields := []string{
		"smtp-user", "smtp-password", "email-to", "email-from",
		"slack-webhook", "slack-channel", "save-folder",
		"container", "service", "image", "user", "network",
		"environment", "secrets", "volumes", "working_dir",
		"log-level",
	}
	for _, f := range optionalFields {
		if !cv.isOptionalField(f) {
			t.Errorf("isOptionalField(%q) should be true", f)
		}
	}

	// Required fields should return false
	requiredFields := []string{"schedule", "command", "name", "something-else"}
	for _, f := range requiredFields {
		if cv.isOptionalField(f) {
			t.Errorf("isOptionalField(%q) should be false", f)
		}
	}
}

// ============================================================================
// Tests targeting 9 surviving CONDITIONALS_NEGATION mutations at specific lines:
//   221, 236, 243, 280, 338, 348, 371, 380, 389
//
// Each test exercises BOTH branches of the conditional and uses hard assertions
// (not t.Logf) so that flipping the condition causes a test failure.
// ============================================================================

// ---------------------------------------------------------------------------
// Line 221:11 — if path != ""
// When path is non-empty, fieldPath = path + "." + fieldName.
// When path is empty, fieldPath = fieldName (no dot prefix).
// A CONDITIONALS_NEGATION mutant reverses this: empty path gets prefix,
// non-empty path gets bare name. We detect this by inspecting error field names.
// ---------------------------------------------------------------------------
func TestMut_Line221_PathPrefixCondition(t *testing.T) {
	t.Parallel()

	// Use a struct with NO gcfg/mapstructure tags so the path logic at line 221
	// is the sole determinant of the fieldPath. The field is non-optional and
	// has no default, so an empty value produces a "required" error whose
	// Field we can inspect.
	type NoTag struct {
		SomeField string // unexported fields are skipped; this is exported, no tags
	}

	t.Run("empty_path_produces_bare_field_name", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()

		cv.validateStruct(v, NoTag{SomeField: ""}, "")

		if !v.HasErrors() {
			t.Fatal("empty non-optional field with no default must produce an error")
		}
		field := v.Errors()[0].Field
		if field != "SomeField" {
			t.Errorf("with empty path, field should be 'SomeField', got %q", field)
		}
	})

	t.Run("non_empty_path_produces_dotted_field_name", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()

		cv.validateStruct(v, NoTag{SomeField: ""}, "parent")

		if !v.HasErrors() {
			t.Fatal("empty non-optional field with no default must produce an error")
		}
		field := v.Errors()[0].Field
		if field != "parent.SomeField" {
			t.Errorf("with non-empty path, field should be 'parent.SomeField', got %q", field)
		}
	})
}

// ---------------------------------------------------------------------------
// Line 236:31 — if gcfgTag != "" && gcfgTag != "-"
// When the gcfg tag is a real name, fieldPath is overridden to the gcfg value.
// When gcfg is "" or "-", it falls through to mapstructure or raw field name.
// ---------------------------------------------------------------------------
func TestMut_Line236_GcfgTagCondition(t *testing.T) {
	t.Parallel()

	t.Run("gcfg_tag_overrides_field_path", func(t *testing.T) {
		t.Parallel()
		type S struct {
			Field string `gcfg:"gcfg-name"`
		}
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validateStruct(v, S{Field: ""}, "")

		if !v.HasErrors() {
			t.Fatal("empty non-optional field must produce error")
		}
		if v.Errors()[0].Field != "gcfg-name" {
			t.Errorf("gcfg tag should override field path to 'gcfg-name', got %q", v.Errors()[0].Field)
		}
	})

	t.Run("gcfg_dash_falls_through_to_mapstructure", func(t *testing.T) {
		t.Parallel()
		type S struct {
			Field string `gcfg:"-" mapstructure:"ms-name"`
		}
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validateStruct(v, S{Field: ""}, "")

		if !v.HasErrors() {
			t.Fatal("empty non-optional field must produce error")
		}
		if v.Errors()[0].Field != "ms-name" {
			t.Errorf("gcfg '-' should fall through to mapstructure 'ms-name', got %q", v.Errors()[0].Field)
		}
	})

	t.Run("no_gcfg_tag_uses_mapstructure", func(t *testing.T) {
		t.Parallel()
		type S struct {
			Field string `mapstructure:"ms-only"`
		}
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validateStruct(v, S{Field: ""}, "")

		if !v.HasErrors() {
			t.Fatal("empty non-optional field must produce error")
		}
		if v.Errors()[0].Field != "ms-only" {
			t.Errorf("without gcfg tag, mapstructure 'ms-only' should be used, got %q", v.Errors()[0].Field)
		}
	})
}

// ---------------------------------------------------------------------------
// Line 243:56 — if field.Kind() == reflect.Struct && mapstructureTag != ",squash"
// Non-squash structs are recursed into. Squash structs are not.
// We prove this by placing a required field inside the struct and checking
// whether its validation error appears.
// ---------------------------------------------------------------------------
func TestMut_Line243_NestedStructSquash(t *testing.T) {
	t.Parallel()

	type Inner struct {
		// "schedule" is not optional, has no default → empty = required error
		Schedule string `mapstructure:"schedule"`
	}

	t.Run("non_squash_struct_recurses_into_inner_fields", func(t *testing.T) {
		t.Parallel()
		type Outer struct {
			Nested Inner `mapstructure:"nested"` // NOT squash → recurse
		}
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validateStruct(v, Outer{Nested: Inner{Schedule: ""}}, "")

		// Inner "schedule" should be validated
		found := false
		for _, e := range v.Errors() {
			if e.Field == "schedule" {
				found = true
			}
		}
		if !found {
			t.Error("non-squash struct must recurse; inner 'schedule' field should produce a required error")
		}
	})

	t.Run("squash_struct_does_not_recurse", func(t *testing.T) {
		t.Parallel()
		type Outer struct {
			Inner `mapstructure:",squash"` // squash → do NOT recurse
		}
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validateStruct(v, Outer{Inner: Inner{Schedule: ""}}, "")

		// Inner "schedule" should NOT be recursively validated
		for _, e := range v.Errors() {
			if e.Field == "schedule" {
				t.Error("squash struct must NOT recurse; inner 'schedule' should not be validated")
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Line 280:29 — if defaultTag != "" && str == ""
// When BOTH conditions hold (field has default AND value is empty), skip
// validation. Otherwise, proceed to validate.
// ---------------------------------------------------------------------------
func TestMut_Line280_DefaultTagEmptyString(t *testing.T) {
	t.Parallel()

	t.Run("empty_value_with_default_skips_validation", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		field := reflect.ValueOf("")
		cv.validateStringField(v, field, "schedule", "some-default")

		if v.HasErrors() {
			t.Error("empty value with default tag must skip validation (no error expected)")
		}
	})

	t.Run("non_empty_value_with_default_still_validates", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		field := reflect.ValueOf("GARBAGE_CRON!!!")
		cv.validateStringField(v, field, "schedule", "some-default")

		if !v.HasErrors() {
			t.Error("non-empty value with default tag must still be validated (invalid cron → error)")
		}
	})

	t.Run("empty_value_without_default_on_required_field_errors", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		field := reflect.ValueOf("")
		cv.validateStringField(v, field, "schedule", "") // no default, required

		if !v.HasErrors() {
			t.Error("empty value without default on required field must produce error")
		}
	})

	t.Run("empty_value_without_default_on_optional_field_ok", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		field := reflect.ValueOf("")
		cv.validateStringField(v, field, "image", "") // no default, optional

		if v.HasErrors() {
			t.Error("empty value without default on optional field should not error")
		}
	})
}

// ---------------------------------------------------------------------------
// Line 338:18 — if cv.sanitizer != nil (in validateCronField)
// With sanitizer, BOTH Validator.ValidateCronExpression AND
// sanitizer.ValidateCronExpression run. Without sanitizer, only the basic one.
// We show different error counts or that nil sanitizer does not panic.
// ---------------------------------------------------------------------------
func TestMut_Line338_ValidateCronField_SanitizerNil(t *testing.T) {
	t.Parallel()

	t.Run("nil_sanitizer_must_not_panic", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: nil}
		v := NewValidator()
		// Must not panic
		cv.validateCronField(v, "schedule", "* * * * *")
		if v.HasErrors() {
			t.Error("valid cron with nil sanitizer should pass basic validation")
		}
	})

	t.Run("with_sanitizer_both_validators_run", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validateCronField(v, "schedule", "* * * * *")
		if v.HasErrors() {
			t.Error("valid cron with sanitizer should also pass")
		}
	})

	t.Run("invalid_cron_with_sanitizer_gets_more_errors", func(t *testing.T) {
		t.Parallel()
		invalidCron := "bad-cron"

		cvWith := &Validator2{sanitizer: NewSanitizer()}
		vWith := NewValidator()
		cvWith.validateCronField(vWith, "schedule", invalidCron)

		cvNil := &Validator2{sanitizer: nil}
		vNil := NewValidator()
		cvNil.validateCronField(vNil, "schedule", invalidCron)

		if len(vWith.Errors()) < len(vNil.Errors()) {
			t.Errorf("sanitizer present should produce >= errors (%d) than nil sanitizer (%d)",
				len(vWith.Errors()), len(vNil.Errors()))
		}
	})
}

// ---------------------------------------------------------------------------
// Line 348:18 — if cv.sanitizer != nil (in validateEmailField)
// ---------------------------------------------------------------------------
func TestMut_Line348_ValidateEmailField_SanitizerNil(t *testing.T) {
	t.Parallel()

	t.Run("nil_sanitizer_must_not_panic", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: nil}
		v := NewValidator()
		cv.validateEmailField(v, "email-to", "user@example.com")
		if v.HasErrors() {
			t.Error("valid email with nil sanitizer should pass basic validation")
		}
	})

	t.Run("with_sanitizer_valid_email_passes", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validateEmailField(v, "email-to", "user@example.com")
		if v.HasErrors() {
			t.Error("valid email with sanitizer should also pass")
		}
	})

	t.Run("invalid_email_with_sanitizer_gets_more_errors", func(t *testing.T) {
		t.Parallel()
		badEmail := "not-valid"

		cvWith := &Validator2{sanitizer: NewSanitizer()}
		vWith := NewValidator()
		cvWith.validateEmailField(vWith, "email-to", badEmail)

		cvNil := &Validator2{sanitizer: nil}
		vNil := NewValidator()
		cvNil.validateEmailField(vNil, "email-to", badEmail)

		if len(vWith.Errors()) < len(vNil.Errors()) {
			t.Errorf("sanitizer present should produce >= errors (%d) than nil sanitizer (%d)",
				len(vWith.Errors()), len(vNil.Errors()))
		}
	})
}

// ---------------------------------------------------------------------------
// Line 371:18 — if cv.sanitizer != nil (in validateCommandField)
// The entire body of validateCommandField is guarded by this nil check.
// With nil sanitizer, NO validation happens (no errors even for dangerous input).
// With sanitizer, dangerous commands are caught.
// ---------------------------------------------------------------------------
func TestMut_Line371_ValidateCommandField_SanitizerNil(t *testing.T) {
	t.Parallel()

	dangerousCmd := "echo hello; rm -rf /"

	t.Run("nil_sanitizer_allows_dangerous_command", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: nil}
		v := NewValidator()
		cv.validateCommandField(v, "command", dangerousCmd)
		if v.HasErrors() {
			t.Error("dangerous command with nil sanitizer must NOT produce errors (no validation)")
		}
	})

	t.Run("with_sanitizer_rejects_dangerous_command", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validateCommandField(v, "command", dangerousCmd)
		if !v.HasErrors() {
			t.Error("dangerous command with sanitizer MUST produce errors")
		}
	})

	t.Run("safe_command_passes_with_sanitizer", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validateCommandField(v, "command", "echo hello")
		if v.HasErrors() {
			t.Errorf("safe command with sanitizer should pass: %v", v.Errors())
		}
	})
}

// ---------------------------------------------------------------------------
// Line 380:18 — if cv.sanitizer != nil (in validateImageField)
// Same pattern: entire body guarded by nil check.
// ---------------------------------------------------------------------------
func TestMut_Line380_ValidateImageField_SanitizerNil(t *testing.T) {
	t.Parallel()

	longImage := fmt.Sprintf("%s:latest", strings.Repeat("x", 260))

	t.Run("nil_sanitizer_allows_invalid_image", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: nil}
		v := NewValidator()
		cv.validateImageField(v, "image", longImage)
		if v.HasErrors() {
			t.Error("invalid image with nil sanitizer must NOT produce errors")
		}
	})

	t.Run("with_sanitizer_rejects_invalid_image", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validateImageField(v, "image", longImage)
		if !v.HasErrors() {
			t.Error("overly long image with sanitizer MUST produce errors")
		}
	})

	t.Run("valid_image_passes_with_sanitizer", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validateImageField(v, "image", "nginx:latest")
		if v.HasErrors() {
			t.Errorf("valid image with sanitizer should pass: %v", v.Errors())
		}
	})
}

// ---------------------------------------------------------------------------
// Line 389:18 — if cv.sanitizer != nil (in validatePathField)
// Same pattern: entire body guarded by nil check.
// ---------------------------------------------------------------------------
func TestMut_Line389_ValidatePathField_SanitizerNil(t *testing.T) {
	t.Parallel()

	traversalPath := "/var/log/../../../etc/shadow"

	t.Run("nil_sanitizer_allows_path_traversal", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: nil}
		v := NewValidator()
		cv.validatePathField(v, "save-folder", traversalPath)
		if v.HasErrors() {
			t.Error("path traversal with nil sanitizer must NOT produce errors")
		}
	})

	t.Run("with_sanitizer_rejects_path_traversal", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validatePathField(v, "save-folder", traversalPath)
		if !v.HasErrors() {
			t.Error("path traversal with sanitizer MUST produce errors")
		}
	})

	t.Run("valid_path_passes_with_sanitizer", func(t *testing.T) {
		t.Parallel()
		cv := &Validator2{sanitizer: NewSanitizer()}
		v := NewValidator()
		cv.validatePathField(v, "save-folder", "/var/log")
		if v.HasErrors() {
			t.Errorf("valid path with sanitizer should pass: %v", v.Errors())
		}
	})
}
