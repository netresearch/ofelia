package config

import (
	"fmt"
	"html"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/netresearch/go-cron"
)

// Sanitizer provides input sanitization and validation for security
type Sanitizer struct {
	// Patterns for detecting potentially malicious input
	sqlInjectionPattern   *regexp.Regexp
	shellInjectionPattern *regexp.Regexp
	pathTraversalPattern  *regexp.Regexp
	ldapInjectionPattern  *regexp.Regexp
}

// NewSanitizer creates a new input sanitizer
func NewSanitizer() *Sanitizer {
	return &Sanitizer{
		// SQL injection patterns
		sqlInjectionPattern: regexp.MustCompile(`(?i)(union|select|insert|update|delete|drop|create|alter|exec|` +
			`execute|script|javascript|eval|setTimeout|setInterval|function|onload|onerror|onclick|` +
			`<script|<iframe|<object|<embed|<img)`),

		// Shell command injection patterns
		shellInjectionPattern: regexp.MustCompile(`[;&|<>$` + "`" + `\n\r]|\$\(|\$\{|&&|\|\||>>|<<`),

		// Path traversal patterns
		pathTraversalPattern: regexp.MustCompile(`\.\.[\\/]|\.\.%2[fF]|%2e%2e|\.\.\\|\.\.\/`),

		// LDAP injection patterns
		ldapInjectionPattern: regexp.MustCompile(`[\(\)\*\|\&\!]`),
	}
}

// SanitizeString performs basic string sanitization
func (s *Sanitizer) SanitizeString(input string, maxLength int) (string, error) {
	// Check length
	if len(input) > maxLength {
		return "", fmt.Errorf("input exceeds maximum length of %d characters", maxLength)
	}

	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")

	// Trim whitespace
	input = strings.TrimSpace(input)

	// Check for control characters
	for _, r := range input {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return "", fmt.Errorf("input contains invalid control characters")
		}
	}

	return input, nil
}

// ValidateCommand validates command strings for shell execution
func (s *Sanitizer) ValidateCommand(command string) error {
	// Check for shell injection patterns
	if s.shellInjectionPattern.MatchString(command) {
		return fmt.Errorf("command contains potentially dangerous shell characters")
	}

	// Validate command doesn't contain common dangerous commands
	dangerousCommands := []string{
		"rm -rf", "dd if=", "mkfs", "format", ":(){:|:&};:",
		"wget ", "curl ", "nc ", "telnet ", "/dev/null",
		"chmod 777", "chmod +x", "sudo", "su -",
	}

	lowerCommand := strings.ToLower(command)
	for _, dangerous := range dangerousCommands {
		if strings.Contains(lowerCommand, dangerous) {
			return fmt.Errorf("command contains potentially dangerous operation: %s", dangerous)
		}
	}

	return nil
}

// ValidatePath validates file paths to prevent traversal attacks
func (s *Sanitizer) ValidatePath(path string, allowedBasePath string) error {
	// Check for path traversal attempts
	if s.pathTraversalPattern.MatchString(path) {
		return fmt.Errorf("path contains directory traversal attempt")
	}

	// Clean and resolve the path
	cleanPath := filepath.Clean(path)

	// If an allowed base path is specified, ensure the path is within it
	if allowedBasePath != "" {
		absPath, err := filepath.Abs(cleanPath)
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}

		absBase, err := filepath.Abs(allowedBasePath)
		if err != nil {
			return fmt.Errorf("invalid base path: %w", err)
		}

		// Check if path is within the allowed base path
		if !strings.HasPrefix(absPath, absBase) {
			return fmt.Errorf("path is outside allowed directory")
		}
	}

	// Check for dangerous file extensions
	dangerousExtensions := []string{
		".exe", ".sh", ".bat", ".cmd", ".ps1", ".dll", ".so",
	}

	ext := strings.ToLower(filepath.Ext(cleanPath))
	if slices.Contains(dangerousExtensions, ext) {
		return fmt.Errorf("file extension %s is not allowed", ext)
	}

	return nil
}

// ValidateEnvironmentVar validates environment variable names and values
func (s *Sanitizer) ValidateEnvironmentVar(name, value string) error {
	// Validate variable name
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(name) {
		return fmt.Errorf("invalid environment variable name: %s", name)
	}

	// Check for shell injection in value
	if s.shellInjectionPattern.MatchString(value) {
		return fmt.Errorf("environment variable value contains potentially dangerous characters")
	}

	// Check for excessive length
	if len(value) > 4096 {
		return fmt.Errorf("environment variable value exceeds maximum length")
	}

	return nil
}

// ValidateURL validates URLs to prevent SSRF and other attacks
func (s *Sanitizer) ValidateURL(rawURL string) error {
	// Parse the URL
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// Check scheme
	allowedSchemes := map[string]bool{
		"http":  true,
		"https": true,
	}

	if !allowedSchemes[strings.ToLower(u.Scheme)] {
		return fmt.Errorf("URL scheme %s is not allowed", u.Scheme)
	}

	// Prevent localhost/internal network access (SSRF prevention)
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "0.0.0.0" ||
		strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "172.") || strings.HasSuffix(host, ".local") {
		return fmt.Errorf("URL points to internal/local network")
	}

	// Check for IP address instead of domain (optional, depends on requirements)
	if regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`).MatchString(host) {
		return fmt.Errorf("direct IP addresses are not allowed")
	}

	return nil
}

// ValidateDockerImage validates Docker image names
func (s *Sanitizer) ValidateDockerImage(image string) error {
	// Docker image name regex pattern
	// Format: [registry/]namespace/repository[:tag]
	imagePattern := regexp.MustCompile(`^(?:(?:[a-zA-Z0-9](?:[a-zA-Z0-9-_]*[a-zA-Z0-9])?\.)*` +
		`[a-zA-Z0-9](?:[a-zA-Z0-9-_]*[a-zA-Z0-9])?(?::[0-9]+)?\/)?[a-z0-9]+(?:[._-][a-z0-9]+)*` +
		`(?:\/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127})?(?:@sha256:[a-f0-9]{64})?$`)

	if !imagePattern.MatchString(image) {
		return fmt.Errorf("invalid Docker image name format")
	}

	// Check for suspicious patterns
	if strings.Contains(image, "..") || strings.Contains(image, "//") {
		return fmt.Errorf("Docker image name contains suspicious patterns")
	}

	// Validate length
	if len(image) > 255 {
		return fmt.Errorf("Docker image name exceeds maximum length")
	}

	return nil
}

// ValidateCronExpression validates a cron expression using go-cron's parser.
// This correctly handles all formats: descriptors (@daily), @every intervals,
// standard cron expressions with optional seconds, month/day names (JAN, MON),
// and wraparound ranges (FRI-MON).
func (s *Sanitizer) ValidateCronExpression(expr string) error {
	// Allow ofelia's triggered-only schedule keywords
	if expr == "@triggered" || expr == "@manual" || expr == "@none" {
		return nil
	}

	parseOpts := cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor
	if err := cron.ValidateSpec(expr, parseOpts); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	return nil
}

// SanitizeHTML performs HTML escaping to prevent XSS
func (s *Sanitizer) SanitizeHTML(input string) string {
	return html.EscapeString(input)
}

// ValidateJobName validates job names for safety
func (s *Sanitizer) ValidateJobName(name string) error {
	// Check length
	if len(name) == 0 || len(name) > 100 {
		return fmt.Errorf("job name must be between 1 and 100 characters")
	}

	// Allow only alphanumeric, dash, underscore
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(name) {
		return fmt.Errorf("job name can only contain letters, numbers, dashes, and underscores")
	}

	return nil
}

// ValidateEmailList validates a comma-separated list of email addresses
func (s *Sanitizer) ValidateEmailList(emails string) error {
	if emails == "" {
		return nil
	}

	emailList := strings.Split(emails, ",")
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

	for _, email := range emailList {
		email = strings.TrimSpace(email)
		if !emailRegex.MatchString(email) {
			return fmt.Errorf("invalid email address: %s", email)
		}
	}

	return nil
}
