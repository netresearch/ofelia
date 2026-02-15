package config

import (
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Value   any
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("config validation error for field '%s': %s (value: %v)",
		e.Field, e.Message, e.Value)
}

// ValidationErrors represents multiple validation errors
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	messages := make([]string, 0, len(e))
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
func (v *Validator) AddError(field string, value any, message string) {
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
func (v *Validator) ValidateMinLength(field string, value string, minLength int) {
	if len(value) < minLength {
		v.AddError(field, value, fmt.Sprintf("must be at least %d characters", minLength))
	}
}

// ValidateMaxLength validates maximum string length
func (v *Validator) ValidateMaxLength(field string, value string, maxLength int) {
	if len(value) > maxLength {
		v.AddError(field, value, fmt.Sprintf("must be at most %d characters", maxLength))
	}
}

// ValidateRange validates that a number is within range
func (v *Validator) ValidateRange(field string, value int, minVal, maxVal int) {
	if value < minVal || value > maxVal {
		v.AddError(field, value, fmt.Sprintf("must be between %d and %d", minVal, maxVal))
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
			"@triggered", "@manual", "@none", // triggered-only jobs
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

	if slices.Contains(allowed, value) {
		return
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
type Validator2 struct {
	config    any
	sanitizer *Sanitizer
}

// NewConfigValidator creates a configuration validator
func NewConfigValidator(config any) *Validator2 {
	return &Validator2{
		config:    config,
		sanitizer: NewSanitizer(),
	}
}

// Validate performs validation on the configuration
func (cv *Validator2) Validate() error {
	v := NewValidator()

	// Validate the configuration using reflection to check struct tags and values
	cv.validateStruct(v, cv.config, "")

	if v.HasErrors() {
		return v.Errors()
	}

	return nil
}

// validateStruct recursively validates struct fields based on tags
func (cv *Validator2) validateStruct(v *Validator, obj any, path string) {
	val := reflect.ValueOf(obj)
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return
	}

	typ := val.Type()
	for i := range val.NumField() {
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
		cv.validateField(v, field, fieldPath, defaultTag)
	}
}

// validateField validates individual fields based on their type and tags
func (cv *Validator2) validateField(v *Validator, field reflect.Value, path string, defaultTag string) {
	switch field.Kind() {
	case reflect.String:
		cv.validateStringField(v, field, path, defaultTag)
	case reflect.Int, reflect.Int64:
		cv.validateIntField(v, field, path)
	case reflect.Slice:
		cv.validateSliceField(v, field, path)
	case reflect.Invalid, reflect.Bool, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Struct, reflect.UnsafePointer:
		// These types are not currently validated or are handled elsewhere (e.g., structs)
		// No validation needed for these field types in this context
	default:
		// Handle any future or unexpected types gracefully
		// No validation performed for unrecognized types
	}
}

// validateStringField validates string type fields
func (cv *Validator2) validateStringField(v *Validator, field reflect.Value, path string, defaultTag string) {
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
func (cv *Validator2) validateSpecificStringField(v *Validator, path string, str string) {
	// First perform general security validation
	if !cv.performSecurityValidation(v, path, str) {
		return // Stop validation if security check fails
	}

	// Validate based on field type
	switch path {
	case "schedule", "cron":
		cv.validateCronField(v, path, str)
	case "email-to", "email-from":
		cv.validateEmailField(v, path, str)
	case "web-address", "pprof-address":
		cv.validateAddressField(v, path, str)
	case "log-level":
		cv.validateLogLevelField(v, path, str)
	case "command", "cmd":
		cv.validateCommandField(v, path, str)
	case "image":
		cv.validateImageField(v, path, str)
	case "save-folder", "working_dir":
		cv.validatePathField(v, path, str)
	}
}

// performSecurityValidation performs general security validation for all string fields
func (cv *Validator2) performSecurityValidation(v *Validator, path string, str string) bool {
	if cv.sanitizer == nil {
		return true
	}

	// General string sanitization for all fields
	if _, err := cv.sanitizer.SanitizeString(str, 1024); err != nil {
		v.AddError(path, str, fmt.Sprintf("input validation failed: %v", err))
		return false
	}
	return true
}

// validateCronField validates cron expression fields
func (cv *Validator2) validateCronField(v *Validator, path string, str string) {
	v.ValidateCronExpression(path, str)
	if cv.sanitizer != nil {
		if err := cv.sanitizer.ValidateCronExpression(str); err != nil {
			v.AddError(path, str, fmt.Sprintf("cron validation failed: %v", err))
		}
	}
}

// validateEmailField validates email fields
func (cv *Validator2) validateEmailField(v *Validator, path string, str string) {
	v.ValidateEmail(path, str)
	if cv.sanitizer != nil {
		if err := cv.sanitizer.ValidateEmailList(str); err != nil {
			v.AddError(path, str, fmt.Sprintf("email validation failed: %v", err))
		}
	}
}

// validateAddressField validates address fields
func (cv *Validator2) validateAddressField(v *Validator, path string, str string) {
	if !cv.isValidAddress(str) {
		v.AddError(path, str, "invalid address format")
	}
}

// validateLogLevelField validates log level fields
func (cv *Validator2) validateLogLevelField(v *Validator, path string, str string) {
	if !cv.isValidLogLevel(str) {
		v.AddError(path, str, "invalid log level (use: debug, info, warning, error, critical)")
	}
}

// validateCommandField validates command fields
func (cv *Validator2) validateCommandField(v *Validator, path string, str string) {
	if cv.sanitizer != nil {
		if err := cv.sanitizer.ValidateCommand(str); err != nil {
			v.AddError(path, str, fmt.Sprintf("command validation failed: %v", err))
		}
	}
}

// validateImageField validates Docker image fields
func (cv *Validator2) validateImageField(v *Validator, path string, str string) {
	if cv.sanitizer != nil {
		if err := cv.sanitizer.ValidateDockerImage(str); err != nil {
			v.AddError(path, str, fmt.Sprintf("Docker image validation failed: %v", err))
		}
	}
}

// validatePathField validates path fields
func (cv *Validator2) validatePathField(v *Validator, path string, str string) {
	if cv.sanitizer != nil {
		if err := cv.sanitizer.ValidatePath(str, ""); err != nil {
			v.AddError(path, str, fmt.Sprintf("path validation failed: %v", err))
		}
	}
}

// validateIntField validates integer type fields
func (cv *Validator2) validateIntField(v *Validator, field reflect.Value, path string) {
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
func (cv *Validator2) validateSliceField(v *Validator, field reflect.Value, path string) {
	// Validate slice fields (e.g., dependencies)
	// Dependencies validation would need access to all job names - deferred to runtime
}

// isOptionalField checks if a field is optional (can be empty)
func (cv *Validator2) isOptionalField(path string) bool {
	optionalFields := []string{
		"smtp-user", "smtp-password", "email-to", "email-from",
		"slack-webhook", "slack-channel", "save-folder",
		"restore-history", "restore-history-max-age",
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
func (cv *Validator2) isValidAddress(addr string) bool {
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
func (cv *Validator2) isValidLogLevel(level string) bool {
	validLevels := []string{"debug", "info", "notice", "warning", "error", "critical"}
	level = strings.ToLower(level)
	return slices.Contains(validLevels, level)
}
