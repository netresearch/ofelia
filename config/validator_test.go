package config

import (
	"strings"
	"testing"
)

func TestValidator(t *testing.T) {
	v := NewValidator()

	if v == nil {
		t.Fatal("NewValidator returned nil")
	}

	if v.HasErrors() {
		t.Error("New validator should not have errors")
	}

	// Add an error
	v.AddError("test_field", "test_value", "test error")

	if !v.HasErrors() {
		t.Error("Validator should have errors after adding one")
	}

	errors := v.Errors()
	if len(errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(errors))
	}

	if errors[0].Field != "test_field" {
		t.Errorf("Expected field 'test_field', got '%s'", errors[0].Field)
	}

	t.Log("Basic validator test passed")
}

func TestValidateRequired(t *testing.T) {
	v := NewValidator()

	// Test empty value
	v.ValidateRequired("field1", "")
	if !v.HasErrors() {
		t.Error("Expected error for empty required field")
	}

	// Test whitespace only
	v = NewValidator()
	v.ValidateRequired("field2", "   ")
	if !v.HasErrors() {
		t.Error("Expected error for whitespace-only required field")
	}

	// Test valid value
	v = NewValidator()
	v.ValidateRequired("field3", "value")
	if v.HasErrors() {
		t.Error("Should not have error for non-empty required field")
	}

	t.Log("ValidateRequired test passed")
}

func TestValidateMinMaxLength(t *testing.T) {
	v := NewValidator()

	// Test min length
	v.ValidateMinLength("field1", "ab", 3)
	if !v.HasErrors() {
		t.Error("Expected error for string shorter than minimum")
	}

	v = NewValidator()
	v.ValidateMinLength("field2", "abc", 3)
	if v.HasErrors() {
		t.Error("Should not have error for string at minimum length")
	}

	// Test max length
	v = NewValidator()
	v.ValidateMaxLength("field3", "abcdef", 5)
	if !v.HasErrors() {
		t.Error("Expected error for string longer than maximum")
	}

	v = NewValidator()
	v.ValidateMaxLength("field4", "abcde", 5)
	if v.HasErrors() {
		t.Error("Should not have error for string at maximum length")
	}

	t.Log("ValidateMinMaxLength test passed")
}

func TestValidateRange(t *testing.T) {
	v := NewValidator()

	// Test below range
	v.ValidateRange("field1", 5, 10, 20)
	if !v.HasErrors() {
		t.Error("Expected error for value below range")
	}

	// Test above range
	v = NewValidator()
	v.ValidateRange("field2", 25, 10, 20)
	if !v.HasErrors() {
		t.Error("Expected error for value above range")
	}

	// Test within range
	v = NewValidator()
	v.ValidateRange("field3", 15, 10, 20)
	if v.HasErrors() {
		t.Error("Should not have error for value within range")
	}

	// Test at boundaries
	v = NewValidator()
	v.ValidateRange("field4", 10, 10, 20)
	v.ValidateRange("field5", 20, 10, 20)
	if v.HasErrors() {
		t.Error("Should not have error for values at range boundaries")
	}

	t.Log("ValidateRange test passed")
}

func TestValidatePositive(t *testing.T) {
	v := NewValidator()

	v.ValidatePositive("field1", 0)
	if !v.HasErrors() {
		t.Error("Expected error for zero value")
	}

	v = NewValidator()
	v.ValidatePositive("field2", -5)
	if !v.HasErrors() {
		t.Error("Expected error for negative value")
	}

	v = NewValidator()
	v.ValidatePositive("field3", 10)
	if v.HasErrors() {
		t.Error("Should not have error for positive value")
	}

	t.Log("ValidatePositive test passed")
}

func TestValidateURL(t *testing.T) {
	testCases := []struct {
		url   string
		valid bool
	}{
		{"", true}, // Empty is allowed
		{"http://example.com", true},
		{"https://example.com/path", true},
		{"ftp://files.example.com", true},
		{"not-a-url", false},
		{"http://", false},
		{"//example.com", false},
	}

	for _, tc := range testCases {
		v := NewValidator()
		v.ValidateURL("url", tc.url)

		hasError := v.HasErrors()
		if tc.valid && hasError {
			t.Errorf("URL '%s' should be valid but got error", tc.url)
		}
		if !tc.valid && !hasError {
			t.Errorf("URL '%s' should be invalid but no error", tc.url)
		}
	}

	t.Log("ValidateURL test passed")
}

func TestValidateEmail(t *testing.T) {
	testCases := []struct {
		email string
		valid bool
	}{
		{"", true}, // Empty is allowed
		{"user@example.com", true},
		{"user.name@example.com", true},
		{"user+tag@example.co.uk", true},
		{"invalid", false},
		{"@example.com", false},
		{"user@", false},
		{"user@.com", false},
	}

	for _, tc := range testCases {
		v := NewValidator()
		v.ValidateEmail("email", tc.email)

		hasError := v.HasErrors()
		if tc.valid && hasError {
			t.Errorf("Email '%s' should be valid but got error", tc.email)
		}
		if !tc.valid && !hasError {
			t.Errorf("Email '%s' should be invalid but no error", tc.email)
		}
	}

	t.Log("ValidateEmail test passed")
}

func TestValidateCronExpression(t *testing.T) {
	testCases := []struct {
		cron  string
		valid bool
	}{
		{"", true}, // Empty is allowed
		{"* * * * *", true},
		{"0 0 * * *", true},
		{"0 0 * * * *", true}, // 6 fields
		{"@daily", true},
		{"@every 5m", true},
		{"@hourly", true},
		{"invalid", false},
		{"* * * *", false},       // Too few fields
		{"* * * * * * *", false}, // Too many fields
		{"@invalid", false},
	}

	for _, tc := range testCases {
		v := NewValidator()
		v.ValidateCronExpression("cron", tc.cron)

		hasError := v.HasErrors()
		if tc.valid && hasError {
			t.Errorf("Cron '%s' should be valid but got error", tc.cron)
		}
		if !tc.valid && !hasError {
			t.Errorf("Cron '%s' should be invalid but no error", tc.cron)
		}
	}

	t.Log("ValidateCronExpression test passed")
}

func TestValidateEnum(t *testing.T) {
	v := NewValidator()
	allowed := []string{"option1", "option2", "option3"}

	// Test valid value
	v.ValidateEnum("field1", "option2", allowed)
	if v.HasErrors() {
		t.Error("Should not have error for valid enum value")
	}

	// Test invalid value
	v = NewValidator()
	v.ValidateEnum("field2", "invalid", allowed)
	if !v.HasErrors() {
		t.Error("Expected error for invalid enum value")
	}

	// Test empty (allowed)
	v = NewValidator()
	v.ValidateEnum("field3", "", allowed)
	if v.HasErrors() {
		t.Error("Empty value should be allowed for enum")
	}

	t.Log("ValidateEnum test passed")
}

func TestValidationError(t *testing.T) {
	err := ValidationError{
		Field:   "test_field",
		Value:   "test_value",
		Message: "is invalid",
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "test_field") {
		t.Error("Error message should contain field name")
	}
	if !strings.Contains(errStr, "is invalid") {
		t.Error("Error message should contain validation message")
	}
	if !strings.Contains(errStr, "test_value") {
		t.Error("Error message should contain value")
	}

	t.Log("ValidationError test passed")
}

func TestValidationErrors(t *testing.T) {
	errors := ValidationErrors{
		{Field: "field1", Value: "val1", Message: "error1"},
		{Field: "field2", Value: "val2", Message: "error2"},
	}

	errStr := errors.Error()
	if !strings.Contains(errStr, "field1") || !strings.Contains(errStr, "field2") {
		t.Error("Combined error message should contain all field names")
	}

	t.Log("ValidationErrors test passed")
}
