package cli

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

// ErrValidationFailed is returned when struct validation fails.
var ErrValidationFailed = errors.New("validation failed")

// configValidator is the package-level validator instance
var configValidator *validator.Validate

func init() {
	configValidator = validator.New()

	// Register custom validators
	_ = configValidator.RegisterValidation("cron", validateCron)
	_ = configValidator.RegisterValidation("dockerimage", validateDockerImage)
	_ = configValidator.RegisterValidation("duration_gte", validateDurationGTE)
}

// ValidateConfig validates a configuration struct using struct tags
func ValidateConfig(cfg interface{}) error {
	err := configValidator.Struct(cfg)
	if err == nil {
		return nil
	}

	// Convert validation errors to user-friendly format
	var validationErrors validator.ValidationErrors
	if !errors.As(err, &validationErrors) {
		return fmt.Errorf("%w: %w", ErrValidationFailed, err)
	}

	messages := make([]string, 0, len(validationErrors))
	for _, e := range validationErrors {
		msg := formatValidationError(e)
		messages = append(messages, msg)
	}

	return fmt.Errorf("%w:\n  %s", ErrValidationFailed, strings.Join(messages, "\n  "))
}

// formatValidationError formats a single validation error for display
func formatValidationError(e validator.FieldError) string {
	field := e.Field()
	tag := e.Tag()
	param := e.Param()
	value := e.Value()

	switch tag {
	case "required":
		return fmt.Sprintf("%s: required field is empty", field)
	case "gte":
		return fmt.Sprintf("%s: must be >= %s (got: %v)", field, param, value)
	case "lte":
		return fmt.Sprintf("%s: must be <= %s (got: %v)", field, param, value)
	case "min":
		return fmt.Sprintf("%s: must be at least %s characters (got: %v)", field, param, value)
	case "max":
		return fmt.Sprintf("%s: must be at most %s characters (got: %v)", field, param, value)
	case "oneof":
		return fmt.Sprintf("%s: must be one of [%s] (got: %v)", field, param, value)
	case "url":
		return fmt.Sprintf("%s: must be a valid URL (got: %v)", field, value)
	case "email":
		return fmt.Sprintf("%s: must be a valid email (got: %v)", field, value)
	case "cron":
		return fmt.Sprintf("%s: must be a valid cron expression (got: %v)", field, value)
	case "dockerimage":
		return fmt.Sprintf("%s: must be a valid Docker image reference (got: %v)", field, value)
	case "duration_gte":
		return fmt.Sprintf("%s: duration must be >= %s (got: %v)", field, param, value)
	default:
		return fmt.Sprintf("%s: validation '%s' failed (got: %v)", field, tag, value)
	}
}

// validateCron validates a cron expression
func validateCron(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	if value == "" {
		return true // Empty is valid (required check handles this)
	}

	// Allow special expressions
	if strings.HasPrefix(value, "@") {
		validSpecial := []string{
			"@yearly", "@annually", "@monthly", "@weekly",
			"@daily", "@midnight", "@hourly",
			"@triggered", "@manual", "@none",
		}

		for _, special := range validSpecial {
			if value == special {
				return true
			}
		}

		// Allow @every with duration
		if strings.HasPrefix(value, "@every ") {
			_, err := time.ParseDuration(strings.TrimPrefix(value, "@every "))
			return err == nil
		}

		return false
	}

	// Standard cron: 5 or 6 fields
	parts := strings.Fields(value)
	if len(parts) < 5 || len(parts) > 6 {
		return false
	}

	// Basic validation: each field has valid cron characters
	cronFieldRegex := regexp.MustCompile(`^[\d\*\-,/\?LW#]+$`)
	for _, part := range parts {
		if !cronFieldRegex.MatchString(part) {
			return false
		}
	}

	return true
}

// validateDockerImage validates a Docker image reference
func validateDockerImage(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	if value == "" {
		return true // Empty is valid (required check handles this)
	}

	// Docker image reference pattern
	// Supports: image, image:tag, registry/image, registry/image:tag, registry:port/image:tag
	// Also supports digest: image@sha256:...
	const imagePattern = `^([a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?` +
		`(/[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?)*)(:[a-zA-Z0-9._-]+)?` +
		`(@sha256:[a-f0-9]{64})?$`
	imageRegex := regexp.MustCompile(imagePattern)

	// Handle registry with port: registry:port/image
	if strings.Contains(value, ":") && strings.Contains(value, "/") {
		// Split on first slash
		parts := strings.SplitN(value, "/", 2)
		if len(parts) == 2 {
			// Check if first part looks like host:port
			if strings.Contains(parts[0], ":") {
				// Validate port part
				hostPort := strings.SplitN(parts[0], ":", 2)
				if len(hostPort) == 2 {
					// Port should be numeric or omitted
					if hostPort[1] != "" {
						for _, c := range hostPort[1] {
							if c < '0' || c > '9' {
								return false
							}
						}
					}
				}
			}
		}
	}

	return imageRegex.MatchString(value) || strings.Contains(value, "/")
}

// validateDurationGTE validates that a duration is >= a minimum value
func validateDurationGTE(fl validator.FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	// Handle time.Duration fields
	if dur, ok := field.Interface().(time.Duration); ok {
		minDur, err := time.ParseDuration(param)
		if err != nil {
			return false
		}
		return dur >= minDur
	}

	// Handle int64 (underlying type of time.Duration)
	if field.Kind().String() == "int64" {
		dur := time.Duration(field.Int())
		minDur, err := time.ParseDuration(param)
		if err != nil {
			return false
		}
		return dur >= minDur
	}

	return true
}

// UnknownKeyWarning represents a warning about an unknown configuration key
type UnknownKeyWarning struct {
	Section    string
	Key        string
	Suggestion string // "did you mean?" suggestion, if available
}

// GenerateUnknownKeyWarnings generates warnings for unknown keys with suggestions
func GenerateUnknownKeyWarnings(section string, unusedKeys []string, knownKeys []string) []UnknownKeyWarning {
	warnings := make([]UnknownKeyWarning, 0, len(unusedKeys))

	for _, key := range unusedKeys {
		warning := UnknownKeyWarning{
			Section: section,
			Key:     key,
		}

		// Find closest match for "did you mean?" suggestion
		if suggestion := findClosestMatch(key, knownKeys); suggestion != "" {
			warning.Suggestion = suggestion
		}

		warnings = append(warnings, warning)
	}

	return warnings
}

// findClosestMatch finds the closest matching key using simple edit distance
func findClosestMatch(key string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}

	key = strings.ToLower(key)
	bestMatch := ""
	bestDistance := len(key) + 1 // Max possible distance

	for _, candidate := range candidates {
		candidate = strings.ToLower(candidate)
		distance := levenshteinDistance(key, candidate)

		// Only suggest if reasonably close (< 3 edits or < 40% of key length)
		threshold := 3
		if len(key) > 5 {
			threshold = len(key) * 2 / 5
		}

		if distance < bestDistance && distance <= threshold {
			bestDistance = distance
			bestMatch = candidate
		}
	}

	return bestMatch
}

// levenshteinDistance calculates the edit distance between two strings
func levenshteinDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	// Create distance matrix
	matrix := make([][]int, len(a)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(b)+1)
		matrix[i][0] = i
	}
	for j := range matrix[0] {
		matrix[0][j] = j
	}

	// Fill in the matrix
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}

			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(a)][len(b)]
}
