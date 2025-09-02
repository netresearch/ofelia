package config

import (
	"fmt"
	"net/url"
	"regexp"
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
		validSpecial := []string{"@yearly", "@annually", "@monthly", "@weekly", 
			"@daily", "@midnight", "@hourly", "@every"}
		
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
	
	// Here you would add specific validation for your config structure
	// This is just an example framework
	
	if v.HasErrors() {
		return v.Errors()
	}
	
	return nil
}