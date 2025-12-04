package config

import (
	"fmt"
	"html"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
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
	for _, dangerous := range dangerousExtensions {
		if ext == dangerous {
			return fmt.Errorf("file extension %s is not allowed", ext)
		}
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

// ValidateCronExpression performs thorough cron expression validation
func (s *Sanitizer) ValidateCronExpression(expr string) error {
	// Handle special expressions
	if strings.HasPrefix(expr, "@") {
		validSpecial := map[string]bool{
			"@yearly":    true,
			"@annually":  true,
			"@monthly":   true,
			"@weekly":    true,
			"@daily":     true,
			"@midnight":  true,
			"@hourly":    true,
			"@triggered": true, // triggered-only jobs (run via workflow or manual)
			"@manual":    true, // alias for @triggered
			"@none":      true, // alias for @triggered
		}

		// Handle @every expressions
		if strings.HasPrefix(expr, "@every ") {
			duration := strings.TrimPrefix(expr, "@every ")
			// Validate duration format
			if !regexp.MustCompile(`^\d+[smhd]$`).MatchString(duration) {
				return fmt.Errorf("invalid @every duration format")
			}
			return nil
		}

		if !validSpecial[expr] {
			return fmt.Errorf("invalid special cron expression: %s", expr)
		}
		return nil
	}

	// Standard cron expression validation
	fields := strings.Fields(expr)
	if len(fields) < 5 || len(fields) > 6 {
		return fmt.Errorf("cron expression must have 5 or 6 fields")
	}

	// Validate each field
	limits := []struct {
		min, max int
		name     string
	}{
		{0, 59, "minute"},     // Minutes
		{0, 23, "hour"},       // Hours
		{1, 31, "day"},        // Day of month
		{1, 12, "month"},      // Month
		{0, 7, "day of week"}, // Day of week (0 and 7 are Sunday)
	}

	// If 6 fields, first is seconds
	if len(fields) == 6 {
		limits = append([]struct {
			min, max int
			name     string
		}{{0, 59, "second"}}, limits...)
	}

	for i, field := range fields {
		if i >= len(limits) {
			break
		}

		if err := s.validateCronField(field, limits[i].min, limits[i].max, limits[i].name); err != nil {
			return err
		}
	}

	return nil
}

// validateCronField validates a single cron field
func (s *Sanitizer) validateCronField(field string, minVal, maxVal int, fieldName string) error {
	// Allow wildcards and question marks
	if field == "*" || field == "?" {
		return nil
	}

	// Check for ranges (e.g., "1-5")
	if strings.Contains(field, "-") {
		return s.validateCronRange(field, minVal, maxVal, fieldName)
	}

	// Check for steps (e.g., "*/5")
	if strings.Contains(field, "/") {
		return s.validateCronStep(field, minVal, maxVal, fieldName)
	}

	// Check for lists (e.g., "1,3,5")
	if strings.Contains(field, ",") {
		return s.validateCronList(field, minVal, maxVal, fieldName)
	}

	return nil
}

// validateCronRange validates cron range expressions like "1-5"
func (s *Sanitizer) validateCronRange(field string, minVal, maxVal int, fieldName string) error {
	parts := strings.Split(field, "-")
	if len(parts) != 2 {
		return fmt.Errorf("invalid range in %s field", fieldName)
	}

	// Validate both range values
	startVal, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || startVal < minVal || startVal > maxVal {
		return fmt.Errorf("invalid start value in %s field range", fieldName)
	}

	endVal, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || endVal < minVal || endVal > maxVal {
		return fmt.Errorf("invalid end value in %s field range", fieldName)
	}

	if startVal >= endVal {
		return fmt.Errorf("invalid range: start value must be less than end value in %s field", fieldName)
	}

	return nil
}

// validateCronStep validates cron step expressions like "*/5" or "0/10"
func (s *Sanitizer) validateCronStep(field string, minVal, maxVal int, fieldName string) error {
	parts := strings.Split(field, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid step in %s field", fieldName)
	}

	// Validate step value
	stepVal, err := strconv.Atoi(parts[1])
	if err != nil || stepVal <= 0 {
		return fmt.Errorf("invalid step value in %s field", fieldName)
	}

	// Validate base value (can be "*" or a number)
	if parts[0] != "*" {
		baseVal, err := strconv.Atoi(parts[0])
		if err != nil || baseVal < minVal || baseVal > maxVal {
			return fmt.Errorf("invalid base value in %s field step", fieldName)
		}
	}

	return nil
}

// validateCronList validates cron list expressions like "1,3,5"
func (s *Sanitizer) validateCronList(field string, minVal, maxVal int, fieldName string) error {
	values := strings.Split(field, ",")
	for _, val := range values {
		val = strings.TrimSpace(val)
		intVal, err := strconv.Atoi(val)
		if err != nil || intVal < minVal || intVal > maxVal {
			return fmt.Errorf("invalid value %s in %s field list", val, fieldName)
		}
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
