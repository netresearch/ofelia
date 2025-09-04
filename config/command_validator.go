package config

import (
	"fmt"
	"regexp"
	"strings"
)

// CommandValidator provides security validation for command arguments
type CommandValidator struct {
	// Allowed characters in Docker service names and file paths
	serviceNamePattern *regexp.Regexp
	filePathPattern    *regexp.Regexp
	// Dangerous patterns to block
	dangerousPatterns []*regexp.Regexp
}

// NewCommandValidator creates a new command validator with security rules
func NewCommandValidator() *CommandValidator {
	return &CommandValidator{
		// Docker service names: alphanumeric, underscore, hyphen, dot
		serviceNamePattern: regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`),
		// File paths: alphanumeric, underscore, hyphen, dot, forward slash
		filePathPattern: regexp.MustCompile(`^[a-zA-Z0-9_\-\./]+$`),
		// Patterns that could indicate command injection attempts
		dangerousPatterns: []*regexp.Regexp{
			regexp.MustCompile(`\$\(`),       // Command substitution $(...)
			regexp.MustCompile("`"),          // Backtick command substitution
			regexp.MustCompile(`\|`),         // Pipe to command
			regexp.MustCompile(`;`),          // Command separator
			regexp.MustCompile(`&{1,2}`),     // Background or AND operator
			regexp.MustCompile(`>`),          // Redirect output
			regexp.MustCompile(`<`),          // Redirect input
			regexp.MustCompile(`\.\./\.\./`), // Directory traversal attempts
			regexp.MustCompile(`\x00`),       // Null byte injection
		},
	}
}

// ValidateServiceName validates a Docker service name for safety
func (v *CommandValidator) ValidateServiceName(service string) error {
	if service == "" {
		return fmt.Errorf("service name cannot be empty")
	}

	if len(service) > 255 {
		return fmt.Errorf("service name too long (max 255 characters)")
	}

	// Check for dangerous patterns first (before character validation)
	for _, pattern := range v.dangerousPatterns {
		if pattern.MatchString(service) {
			return fmt.Errorf("service name contains dangerous pattern: %s", service)
		}
	}

	// Then check for valid characters
	if !v.serviceNamePattern.MatchString(service) {
		return fmt.Errorf("service name contains invalid characters: %s", service)
	}

	return nil
}

// ValidateFilePath validates a Docker compose file path for safety
func (v *CommandValidator) ValidateFilePath(path string) error {
	if path == "" {
		return fmt.Errorf("file path cannot be empty")
	}

	if len(path) > 4096 {
		return fmt.Errorf("file path too long (max 4096 characters)")
	}

	// Normalize path to prevent tricks
	path = strings.ReplaceAll(path, "//", "/")

	// Check for dangerous patterns first (before character validation)
	for _, pattern := range v.dangerousPatterns {
		if pattern.MatchString(path) {
			return fmt.Errorf("file path contains dangerous pattern: %s", path)
		}
	}

	// Check for sensitive directories
	sensitivePrefix := []string{"/etc/", "/proc/", "/sys/", "/dev/"}
	for _, prefix := range sensitivePrefix {
		if strings.HasPrefix(path, prefix) {
			return fmt.Errorf("file path attempts to access sensitive directory: %s", path)
		}
	}

	// Then check for valid characters
	if !v.filePathPattern.MatchString(path) {
		return fmt.Errorf("file path contains invalid characters: %s", path)
	}

	return nil
}

// ValidateCommandArgs validates command arguments for safety
func (v *CommandValidator) ValidateCommandArgs(args []string) error {
	for i, arg := range args {
		if len(arg) > 4096 {
			return fmt.Errorf("argument %d too long (max 4096 characters)", i)
		}

		for _, pattern := range v.dangerousPatterns {
			if pattern.MatchString(arg) {
				return fmt.Errorf("argument %d contains dangerous pattern: %s", i, arg)
			}
		}

		// Check for null bytes
		if strings.Contains(arg, "\x00") {
			return fmt.Errorf("argument %d contains null byte", i)
		}
	}

	return nil
}

// SanitizeCommand removes potentially dangerous characters from a command string
func (v *CommandValidator) SanitizeCommand(cmd string) string {
	// Remove null bytes
	cmd = strings.ReplaceAll(cmd, "\x00", "")

	// Limit length
	if len(cmd) > 4096 {
		cmd = cmd[:4096]
	}

	return cmd
}
