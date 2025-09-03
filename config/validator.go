package config

import (
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("config validation error for field '%s': %s (value: %v)",
		e.Field, e.Message, e.Value)
}

// ValidationErrors represents multiple validation errors
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	var messages []string
	for _, err := range e {
		messages = append(messages, err.Error())
	}
	return strings.Join(messages, "; ")
}

// Validator provides configuration validation
type Validator struct {
	errors ValidationErrors
}

// NewValidator creates a new configuration validator
func NewValidator() *Validator {
	return &Validator{
		errors: make(ValidationErrors, 0),
	}
}

// AddError adds a validation error
func (v *Validator) AddError(field string, value interface{}, message string) {
	v.errors = append(v.errors, ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
	})
}

// HasErrors returns true if there are validation errors
func (v *Validator) HasErrors() bool {
	return len(v.errors) > 0
}

// Errors returns all validation errors
func (v *Validator) Errors() ValidationErrors {
	return v.errors
}

// ValidateRequired validates that a field is not empty
func (v *Validator) ValidateRequired(field string, value string) {
	if strings.TrimSpace(value) == "" {
		v.AddError(field, value, "is required")
	}
}

// ValidateMinLength validates minimum string length
func (v *Validator) ValidateMinLength(field string, value string, min int) {
	if len(value) < min {
		v.AddError(field, value, fmt.Sprintf("must be at least %d characters", min))
	}
}

// ValidateMaxLength validates maximum string length
func (v *Validator) ValidateMaxLength(field string, value string, max int) {
	if len(value) > max {
		v.AddError(field, value, fmt.Sprintf("must be at most %d characters", max))
	}
}

// ValidateRange validates that a number is within range
func (v *Validator) ValidateRange(field string, value int, min, max int) {
	if value < min || value > max {
		v.AddError(field, value, fmt.Sprintf("must be between %d and %d", min, max))
	}
}

// ValidatePositive validates that a number is positive
func (v *Validator) ValidatePositive(field string, value int) {
	if value <= 0 {
		v.AddError(field, value, "must be positive")
	}
}

// ValidateURL validates that a string is a valid URL
func (v *Validator) ValidateURL(field string, value string) {
	if value == "" {
		return
	}

	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		v.AddError(field, value, "must be a valid URL")
	}
}

// ValidateEmail validates that a string is a valid email
func (v *Validator) ValidateEmail(field string, value string) {
	if value == "" {
		return
	}

	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(value) {
		v.AddError(field, value, "must be a valid email address")
	}
}

// ValidateCronExpression validates a cron expression
func (v *Validator) ValidateCronExpression(field string, value string) {
	if value == "" {
		return
	}

	// Basic cron validation (5 or 6 fields)
	// This is a simplified check - a full parser would be more thorough
	parts := strings.Fields(value)

	// Allow special expressions
	if strings.HasPrefix(value, "@") {
		validSpecial := []string{
			"@yearly", "@annually", "@monthly", "@weekly",
			"@daily", "@midnight", "@hourly", "@every",
		}

		isValid := false
		for _, special := range validSpecial {
			if value == special || strings.HasPrefix(value, special+" ") {
				isValid = true
				break
			}
		}

		if !isValid {
			v.AddError(field, value, "invalid special cron expression")
		}
		return
	}

	if len(parts) < 5 || len(parts) > 6 {
		v.AddError(field, value, "must have 5 or 6 fields")
		return
	}

	// Validate each field has valid characters
	cronRegex := regexp.MustCompile(`^[\d\*\-,/]+$`)
	for _, part := range parts {
		if !cronRegex.MatchString(part) && part != "?" {
			v.AddError(field, value, "contains invalid characters")
			return
		}
	}
}

// ValidateEnum validates that a value is in a list of allowed values
func (v *Validator) ValidateEnum(field string, value string, allowed []string) {
	if value == "" {
		return
	}

	for _, a := range allowed {
		if value == a {
			return
		}
	}

	v.AddError(field, value, fmt.Sprintf("must be one of: %s", strings.Join(allowed, ", ")))
}

// ValidatePath validates that a path exists or can be created
func (v *Validator) ValidatePath(field string, value string) {
	if value == "" {
		return
	}

	// Basic path validation - just check for invalid characters
	if strings.ContainsAny(value, "\x00") {
		v.AddError(field, value, "contains invalid characters")
	}
}

// ConfigValidator validates complete configuration
type ConfigValidator struct {
	config interface{}
}

// NewConfigValidator creates a configuration validator
func NewConfigValidator(config interface{}) *ConfigValidator {
	return &ConfigValidator{
		config: config,
	}
}

// Validate performs validation on the configuration
func (cv *ConfigValidator) Validate() error {
	v := NewValidator()

	// Validate the configuration using reflection to check struct tags and values
	cv.validateStruct(v, cv.config, "")

	if v.HasErrors() {
		return v.Errors()
	}

	return nil
}

// validateStruct recursively validates struct fields based on tags
func (cv *ConfigValidator) validateStruct(v *Validator, obj interface{}, path string) {
	val := reflect.ValueOf(obj)
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)
		fieldName := fieldType.Name

		// Build field path for nested structs
		fieldPath := fieldName
		if path != "" {
			fieldPath = path + "." + fieldName
		}

		// Skip unexported fields
		if !field.CanInterface() {
			continue
		}

		// Get field tags
		gcfgTag := fieldType.Tag.Get("gcfg")
		mapstructureTag := fieldType.Tag.Get("mapstructure")
		defaultTag := fieldType.Tag.Get("default")

		// Use gcfg or mapstructure tag as field name if available
		if gcfgTag != "" && gcfgTag != "-" {
			fieldPath = gcfgTag
		} else if mapstructureTag != "" && mapstructureTag != "-" && mapstructureTag != ",squash" {
			fieldPath = mapstructureTag
		}

		// Handle nested structs
		if field.Kind() == reflect.Struct && mapstructureTag != ",squash" {
			cv.validateStruct(v, field.Interface(), fieldPath)
			continue
		}

		// Validate based on field type and value
		cv.validateField(v, field, fieldType, fieldPath, defaultTag)
	}
}

// validateField validates individual fields based on their type and tags
func (cv *ConfigValidator) validateField(v *Validator, field reflect.Value, fieldType reflect.StructField, path string, defaultTag string) {
	switch field.Kind() {
	case reflect.String:
		cv.validateStringField(v, field, path, defaultTag)
	case reflect.Int, reflect.Int64:
		cv.validateIntField(v, field, path)
	case reflect.Slice:
		cv.validateSliceField(v, field, path)
	}
}

// validateStringField validates string type fields
func (cv *ConfigValidator) validateStringField(v *Validator, field reflect.Value, path string, defaultTag string) {
	str := field.String()

	// Skip validation for fields with defaults when they're empty
	if defaultTag != "" && str == "" {
		return
	}

	// Check for required fields
	if defaultTag == "" && str == "" && !cv.isOptionalField(path) {
		v.ValidateRequired(path, str)
	}

	// Validate specific string fields
	if str != "" {
		cv.validateSpecificStringField(v, path, str)
	}
}

// validateSpecificStringField validates specific string field formats
func (cv *ConfigValidator) validateSpecificStringField(v *Validator, path string, str string) {
	switch path {
	case "schedule", "cron":
		v.ValidateCronExpression(path, str)
	case "email-to", "email-from":
		v.ValidateEmail(path, str)
	case "web-address", "pprof-address":
		if !cv.isValidAddress(str) {
			v.AddError(path, str, "invalid address format")
		}
	case "log-level":
		if !cv.isValidLogLevel(str) {
			v.AddError(path, str, "invalid log level (use: debug, info, warning, error, critical)")
		}
	}
}

// validateIntField validates integer type fields
func (cv *ConfigValidator) validateIntField(v *Validator, field reflect.Value, path string) {
	val := field.Int()

	// Validate port numbers
	if strings.Contains(path, "port") && val > 0 {
		v.ValidateRange(path, int(val), 1, 65535)
	}

	// Validate positive values for counts/sizes
	if (strings.Contains(path, "max") || strings.Contains(path, "size")) && val < 0 {
		v.AddError(path, val, "must be non-negative")
	}
}

// validateSliceField validates slice type fields
func (cv *ConfigValidator) validateSliceField(v *Validator, field reflect.Value, path string) {
	// Validate slice fields (e.g., dependencies)
	if field.Len() > 0 && strings.Contains(path, "dependencies") {
		// Dependencies should reference valid job names
		// This would need access to all job names, skipping for now
	}
}

// isOptionalField checks if a field is optional (can be empty)
func (cv *ConfigValidator) isOptionalField(path string) bool {
	optionalFields := []string{
		"smtp-user", "smtp-password", "email-to", "email-from",
		"slack-webhook", "slack-channel", "save-folder",
		"container", "service", "image", "user", "network",
		"environment", "secrets", "volumes", "working_dir",
		"log-level", // Has default value "info"
	}

	for _, field := range optionalFields {
		if strings.Contains(path, field) {
			return true
		}
	}
	return false
}

// isValidAddress checks if an address string is valid
func (cv *ConfigValidator) isValidAddress(addr string) bool {
	// Allow formats like ":8080", "localhost:8080", "127.0.0.1:8080"
	if addr == "" {
		return false
	}

	// Simple validation - must contain colon for port
	if !strings.Contains(addr, ":") {
		return false
	}

	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return false
	}

	// Port must be numeric
	_, err := strconv.Atoi(parts[1])
	return err == nil
}

// isValidLogLevel checks if a log level is valid
func (cv *ConfigValidator) isValidLogLevel(level string) bool {
	validLevels := []string{"debug", "info", "notice", "warning", "error", "critical"}
	level = strings.ToLower(level)
	for _, valid := range validLevels {
		if level == valid {
			return true
		}
	}
	return false
}
