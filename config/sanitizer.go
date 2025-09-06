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
	sqlInjectionPattern     *regexp.Regexp
	shellInjectionPattern   *regexp.Regexp
	pathTraversalPattern    *regexp.Regexp
	ldapInjectionPattern    *regexp.Regexp
	dockerEscapePattern     *regexp.Regexp
	commandInjectionPattern *regexp.Regexp
}

// NewSanitizer creates a new input sanitizer with enhanced security patterns
func NewSanitizer() *Sanitizer {
	return &Sanitizer{
		// SQL injection patterns - enhanced with more attack vectors
		sqlInjectionPattern: regexp.MustCompile(`(?i)(union|select|insert|update|delete|drop|create|alter|exec|` +
			`execute|script|javascript|eval|setTimeout|setInterval|function|onload|onerror|onclick|` +
			`<script|<iframe|<object|<embed|<img|xp_cmdshell|sp_executesql|bulk|openrowset)`),

		// Shell command injection patterns - comprehensive detection
		shellInjectionPattern: regexp.MustCompile(`[;&|<>$` + "`" + `\n\r]|\$\(|\$\{|&&|\|\||>>|<<|` +
			`\$\([^)]*\)|` + "`" + `[^` + "`" + `]*` + "`" + `|nc\s|netcat\s|curl\s|wget\s|python\s|perl\s|ruby\s|php\s`),

		// Path traversal patterns - enhanced detection
		pathTraversalPattern: regexp.MustCompile(`\.\.[\\/]|\.\.%2[fF]|%2e%2e|\.\.\\|\.\.\/|` +
			`%252e%252e|%c0%ae|%c1%9c|\.\.%5c|\.\.%2f`),

		// LDAP injection patterns
		ldapInjectionPattern: regexp.MustCompile(`[\(\)\*\|\&\!]`),

		// Docker escape patterns - detect container breakout attempts
		dockerEscapePattern: regexp.MustCompile(`(?i)(--privileged|--pid\s*=\s*host|--network\s*=\s*host|` +
			`--volume\s+[^:]*:/[^:]*:.*rw|--device\s|/proc/self/|/sys/fs/cgroup|` +
			`--cap-add\s*=\s*(SYS_ADMIN|ALL)|--security-opt\s*=\s*apparmor:unconfined|` +
			`--user\s*=\s*(0|root)|--rm\s|docker\.sock|/var/run/docker\.sock)`),

		// Command injection patterns specific to job execution
		commandInjectionPattern: regexp.MustCompile(`(?i)(rm\s+-rf\s+/|mkfs|dd\s+if=|:.*:|fork\s*bomb|` +
			`/dev/random|/dev/zero|> /dev/|chmod\s+777|chmod\s+\+x\s+/|` +
			`sudo\s|su\s+-|passwd\s|shadow|/etc/passwd|/etc/shadow|` +
			`\bkill\s+-9|killall|pkill|shutdown|reboot|halt|init\s+[016])`),
	}
}

// SanitizeString performs comprehensive string sanitization
func (s *Sanitizer) SanitizeString(input string, maxLength int) (string, error) {
	// Check length first
	if len(input) > maxLength {
		return "", fmt.Errorf("input exceeds maximum length of %d characters", maxLength)
	}

	// Remove null bytes and other dangerous control characters
	input = strings.ReplaceAll(input, "\x00", "")
	input = strings.ReplaceAll(input, "\x01", "")
	input = strings.ReplaceAll(input, "\x02", "")
	input = strings.ReplaceAll(input, "\x03", "")

	// Trim whitespace
	input = strings.TrimSpace(input)

	// Check for dangerous control characters (allow tab, newline, carriage return)
	for _, r := range input {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return "", fmt.Errorf("input contains invalid control characters")
		}
	}

	// Check for encoding attacks
	if strings.Contains(input, "%") {
		decoded, err := url.QueryUnescape(input)
		if err == nil && decoded != input {
			// Check if decoded version has dangerous patterns
			if s.hasSecurityViolation(decoded) {
				return "", fmt.Errorf("input contains encoded security threats")
			}
		}
	}

	return input, nil
}

// hasSecurityViolation checks for common security threat patterns
func (s *Sanitizer) hasSecurityViolation(input string) bool {
	return s.sqlInjectionPattern.MatchString(input) ||
		s.shellInjectionPattern.MatchString(input) ||
		s.pathTraversalPattern.MatchString(input) ||
		s.dockerEscapePattern.MatchString(input) ||
		s.commandInjectionPattern.MatchString(input)
}

// ValidateCommand validates command strings with enhanced security checks
func (s *Sanitizer) ValidateCommand(command string) error {
	if command == "" {
		return fmt.Errorf("command cannot be empty")
	}

	// Check for shell injection patterns
	if s.shellInjectionPattern.MatchString(command) {
		return fmt.Errorf("command contains potentially dangerous shell characters")
	}

	// Check for command injection patterns
	if s.commandInjectionPattern.MatchString(command) {
		return fmt.Errorf("command contains potentially dangerous operations")
	}

	// Check for Docker escape attempts
	if s.dockerEscapePattern.MatchString(command) {
		return fmt.Errorf("command contains potentially dangerous Docker operations")
	}

	// Validate individual command components don't contain dangerous operations
	dangerousCommands := []string{
		// File system destruction
		"rm -rf /", "rm -rf /*", "rm -rf ~", "mkfs", "format", "fdisk",
		
		// Network operations
		"wget ", "curl ", "nc ", "ncat ", "netcat ", "telnet ", "ssh ", "scp ", "rsync ",
		
		// System manipulation
		"chmod 777", "chmod +x /", "chown root", "sudo", "su -", "passwd", "usermod",
		"mount ", "umount ", "modprobe ", "insmod ", "rmmod ",
		
		// Process manipulation  
		"kill -9", "killall", "pkill", "shutdown", "reboot", "halt", "init 0", "init 6",
		
		// Fork bombs and resource exhaustion
		":(){:|:&};:", ":(){ :|:& };:", "fork bomb", "/dev/null &", "> /dev/null &",
		
		// Privilege escalation
		"/etc/passwd", "/etc/shadow", "/etc/sudoers", "/root/", "SUID", "setuid",
		
		// Container escapes
		"docker.sock", "/var/run/docker.sock", "/proc/self/root", "/sys/fs/cgroup",
		"--privileged", "--pid host", "--network host", "--cap-add SYS_ADMIN",
	}

	lowerCommand := strings.ToLower(command)
	for _, dangerous := range dangerousCommands {
		if strings.Contains(lowerCommand, strings.ToLower(dangerous)) {
			return fmt.Errorf("command contains potentially dangerous operation: %s", dangerous)
		}
	}

	// Check command length to prevent excessively long commands
	if len(command) > 4096 {
		return fmt.Errorf("command exceeds maximum length of 4096 characters")
	}

	return nil
}

// ValidateDockerCommand validates Docker-specific command strings
func (s *Sanitizer) ValidateDockerCommand(command string) error {
	if err := s.ValidateCommand(command); err != nil {
		return err
	}

	// Additional Docker-specific validation
	if s.dockerEscapePattern.MatchString(command) {
		return fmt.Errorf("Docker command contains potential container escape patterns")
	}

	// Check for dangerous Docker flags
	dangerousDockerFlags := []string{
		"--privileged",
		"--pid=host", "--pid host",
		"--network=host", "--network host", "--net=host", "--net host",
		"--ipc=host", "--ipc host",
		"--uts=host", "--uts host",
		"--user=0", "--user 0", "--user=root", "--user root",
		"--cap-add=ALL", "--cap-add ALL", "--cap-add=SYS_ADMIN", "--cap-add SYS_ADMIN",
		"--security-opt=apparmor:unconfined", "--security-opt apparmor:unconfined",
		"--security-opt=seccomp:unconfined", "--security-opt seccomp:unconfined",
		"--device=/dev/", "--device /dev/",
	}

	lowerCommand := strings.ToLower(command)
	for _, flag := range dangerousDockerFlags {
		if strings.Contains(lowerCommand, strings.ToLower(flag)) {
			return fmt.Errorf("Docker command contains dangerous flag: %s", flag)
		}
	}

	return nil
}

// ValidatePath validates file paths with enhanced security
func (s *Sanitizer) ValidatePath(path string, allowedBasePath string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Check for path traversal attempts
	if s.pathTraversalPattern.MatchString(path) {
		return fmt.Errorf("path contains directory traversal attempt")
	}

	// Check for encoded path traversal
	decoded, err := url.QueryUnescape(path)
	if err == nil && s.pathTraversalPattern.MatchString(decoded) {
		return fmt.Errorf("path contains encoded directory traversal attempt")
	}

	// Clean and resolve the path
	cleanPath := filepath.Clean(path)

	// Check for dangerous absolute paths
	dangerousPaths := []string{
		"/etc/", "/root/", "/home/", "/var/", "/usr/bin/", "/usr/sbin/", "/bin/", "/sbin/",
		"/proc/", "/sys/", "/dev/", "/boot/", "/lib/", "/lib64/",
		"C:\\Windows\\", "C:\\Program Files\\", "C:\\Users\\",
	}

	for _, dangerous := range dangerousPaths {
		if strings.HasPrefix(strings.ToLower(cleanPath), strings.ToLower(dangerous)) {
			return fmt.Errorf("path points to potentially dangerous system directory: %s", dangerous)
		}
	}

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
		".exe", ".sh", ".bat", ".cmd", ".ps1", ".dll", ".so", ".com", ".scr", ".pif",
		".application", ".gadget", ".msi", ".msp", ".cpl", ".scf", ".lnk", ".inf",
		".reg", ".jar", ".vbs", ".js", ".jse", ".ws", ".wsf", ".wsc", ".wsh",
	}

	ext := strings.ToLower(filepath.Ext(cleanPath))
	for _, dangerous := range dangerousExtensions {
		if ext == dangerous {
			return fmt.Errorf("file extension %s is not allowed for security", ext)
		}
	}

	return nil
}

// ValidateEnvironmentVar validates environment variable names and values
func (s *Sanitizer) ValidateEnvironmentVar(name, value string) error {
	// Validate variable name - strict alphanumeric and underscore only
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(name) {
		return fmt.Errorf("invalid environment variable name: %s", name)
	}

	// Check for reserved/dangerous environment variable names
	dangerousVars := []string{
		"PATH", "LD_LIBRARY_PATH", "LD_PRELOAD", "DYLD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES",
		"PYTHONPATH", "RUBYLIB", "PERL5LIB", "CLASSPATH", "JAVA_HOME", "HOME", "USER",
		"SHELL", "IFS", "PS1", "PS2", "PS3", "PS4", "TERM", "DISPLAY",
	}

	upperName := strings.ToUpper(name)
	for _, dangerous := range dangerousVars {
		if upperName == dangerous {
			return fmt.Errorf("environment variable %s is restricted for security", name)
		}
	}

	// Check for shell injection in value
	if s.shellInjectionPattern.MatchString(value) {
		return fmt.Errorf("environment variable value contains potentially dangerous characters")
	}

	// Check for command injection in value
	if s.commandInjectionPattern.MatchString(value) {
		return fmt.Errorf("environment variable value contains potentially dangerous commands")
	}

	// Check for excessive length
	if len(value) > 4096 {
		return fmt.Errorf("environment variable value exceeds maximum length of 4096 characters")
	}

	return nil
}

// ValidateURL validates URLs with enhanced SSRF protection
func (s *Sanitizer) ValidateURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	// Parse the URL
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// Check scheme - only allow HTTPS in production
	allowedSchemes := map[string]bool{
		"https": true,
		// HTTP only allowed for development - should be disabled in production
		"http": true,
	}

	if !allowedSchemes[strings.ToLower(u.Scheme)] {
		return fmt.Errorf("URL scheme %s is not allowed (only https/http permitted)", u.Scheme)
	}

	// Enhanced SSRF prevention - block internal networks
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "0.0.0.0" || host == "::1" ||
		strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "172.16.") || strings.HasPrefix(host, "172.17.") ||
		strings.HasPrefix(host, "172.18.") || strings.HasPrefix(host, "172.19.") ||
		strings.HasPrefix(host, "172.2") || strings.HasPrefix(host, "172.30.") ||
		strings.HasPrefix(host, "172.31.") || strings.HasSuffix(host, ".local") ||
		strings.HasPrefix(host, "169.254.") || strings.HasPrefix(host, "fd") ||
		strings.HasPrefix(host, "fe80:") {
		return fmt.Errorf("URL points to internal/local network address")
	}

	// Block suspicious patterns
	if strings.Contains(host, "amazonaws.com") && strings.Contains(u.Path, "169.254.169.254") {
		return fmt.Errorf("URL appears to target cloud metadata service")
	}

	// Validate port range
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil {
			return fmt.Errorf("invalid port number: %s", u.Port())
		}
		if port < 1 || port > 65535 {
			return fmt.Errorf("port number out of valid range: %d", port)
		}
		// Block common internal service ports
		dangerousPorts := []int{22, 23, 25, 53, 135, 139, 445, 1433, 1521, 3306, 3389, 5432, 5984, 6379, 9200, 11211, 27017}
		for _, dangerousPort := range dangerousPorts {
			if port == dangerousPort {
				return fmt.Errorf("port %d is restricted for security", port)
			}
		}
	}

	return nil
}

// ValidateDockerImage validates Docker image names with enhanced security
func (s *Sanitizer) ValidateDockerImage(image string) error {
	if image == "" {
		return fmt.Errorf("Docker image name cannot be empty")
	}

	// Docker image name regex pattern - comprehensive validation
	imagePattern := regexp.MustCompile(`^(?:(?:[a-zA-Z0-9](?:[a-zA-Z0-9-_]*[a-zA-Z0-9])?\.)*` +
		`[a-zA-Z0-9](?:[a-zA-Z0-9-_]*[a-zA-Z0-9])?(?::[0-9]+)?\/)?[a-z0-9]+(?:[._-][a-z0-9]+)*` +
		`(?:\/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127})?(?:@sha256:[a-f0-9]{64})?$`)

	if !imagePattern.MatchString(image) {
		return fmt.Errorf("invalid Docker image name format")
	}

	// Check for suspicious patterns that could indicate attacks
	if strings.Contains(image, "..") || strings.Contains(image, "//") {
		return fmt.Errorf("Docker image name contains suspicious traversal patterns")
	}

	// Validate length
	if len(image) > 255 {
		return fmt.Errorf("Docker image name exceeds maximum length of 255 characters")
	}

	// Block potentially malicious registries (this is optional and environment-specific)
	suspiciousPatterns := []string{
		"localhost:", "127.0.0.1:", "0.0.0.0:", "192.168.", "10.", "172.",
	}

	lowerImage := strings.ToLower(image)
	for _, pattern := range suspiciousPatterns {
		if strings.HasPrefix(lowerImage, pattern) {
			return fmt.Errorf("Docker image from potentially suspicious registry: %s", pattern)
		}
	}

	return nil
}

// ValidateCronExpression performs comprehensive cron expression validation
func (s *Sanitizer) ValidateCronExpression(expr string) error {
	if expr == "" {
		return fmt.Errorf("cron expression cannot be empty")
	}

	// Check for malicious patterns in cron expressions
	if s.hasSecurityViolation(expr) {
		return fmt.Errorf("cron expression contains potentially malicious patterns")
	}

	// Handle special expressions
	if strings.HasPrefix(expr, "@") {
		validSpecial := map[string]bool{
			"@yearly":   true,
			"@annually": true,
			"@monthly":  true,
			"@weekly":   true,
			"@daily":    true,
			"@midnight": true,
			"@hourly":   true,
		}

		// Handle @every expressions with validation
		if strings.HasPrefix(expr, "@every ") {
			duration := strings.TrimPrefix(expr, "@every ")
			// Strict validation for duration format
			if !regexp.MustCompile(`^\d+[smhd]$`).MatchString(duration) {
				return fmt.Errorf("invalid @every duration format, use: 1s, 5m, 1h, 1d")
			}
			// Extract number and unit
			numStr := duration[:len(duration)-1]
			unit := duration[len(duration)-1:]
			
			num, err := strconv.Atoi(numStr)
			if err != nil {
				return fmt.Errorf("invalid number in @every duration: %s", numStr)
			}
			
			// Prevent excessively frequent executions (less than 1 second) or too infrequent
			switch unit {
			case "s":
				if num < 1 || num > 86400 { // 1 second to 1 day in seconds
					return fmt.Errorf("@every seconds value must be between 1 and 86400")
				}
			case "m":
				if num < 1 || num > 1440 { // 1 minute to 1 day in minutes
					return fmt.Errorf("@every minutes value must be between 1 and 1440")
				}
			case "h":
				if num < 1 || num > 24 { // 1 hour to 1 day
					return fmt.Errorf("@every hours value must be between 1 and 24")
				}
			case "d":
				if num < 1 || num > 365 { // 1 day to 1 year
					return fmt.Errorf("@every days value must be between 1 and 365")
				}
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
		return fmt.Errorf("cron expression must have 5 or 6 fields, got %d", len(fields))
	}

	// Validate each field according to cron specifications
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
			return fmt.Errorf("field %d (%s): %w", i+1, limits[i].name, err)
		}
	}

	return nil
}

// validateCronField validates a single cron field with comprehensive checks
func (s *Sanitizer) validateCronField(field string, minVal, maxVal int, fieldName string) error {
	// Check for malicious patterns
	if s.hasSecurityViolation(field) {
		return fmt.Errorf("field contains potentially malicious patterns")
	}

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

	// Single numeric value
	if val, err := strconv.Atoi(field); err == nil {
		if val < minVal || val > maxVal {
			return fmt.Errorf("value %d is outside valid range %d-%d", val, minVal, maxVal)
		}
		return nil
	}

	return fmt.Errorf("invalid field value: %s", field)
}

// validateCronRange validates cron range expressions like "1-5"
func (s *Sanitizer) validateCronRange(field string, minVal, maxVal int, fieldName string) error {
	parts := strings.Split(field, "-")
	if len(parts) != 2 {
		return fmt.Errorf("invalid range format in %s field", fieldName)
	}

	// Validate both range values
	startVal, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || startVal < minVal || startVal > maxVal {
		return fmt.Errorf("invalid start value %s in %s field range", parts[0], fieldName)
	}

	endVal, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || endVal < minVal || endVal > maxVal {
		return fmt.Errorf("invalid end value %s in %s field range", parts[1], fieldName)
	}

	if startVal >= endVal {
		return fmt.Errorf("invalid range: start value %d must be less than end value %d", startVal, endVal)
	}

	return nil
}

// validateCronStep validates cron step expressions like "*/5" or "0/10"
func (s *Sanitizer) validateCronStep(field string, minVal, maxVal int, fieldName string) error {
	parts := strings.Split(field, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid step format in %s field", fieldName)
	}

	// Validate step value
	stepVal, err := strconv.Atoi(parts[1])
	if err != nil || stepVal <= 0 {
		return fmt.Errorf("invalid step value %s in %s field", parts[1], fieldName)
	}

	// Step value should not be larger than the field range
	if stepVal > (maxVal - minVal + 1) {
		return fmt.Errorf("step value %d is larger than field range in %s field", stepVal, fieldName)
	}

	// Validate base value (can be "*" or a number)
	if parts[0] != "*" {
		baseVal, err := strconv.Atoi(parts[0])
		if err != nil || baseVal < minVal || baseVal > maxVal {
			return fmt.Errorf("invalid base value %s in %s field step", parts[0], fieldName)
		}
	}

	return nil
}

// validateCronList validates cron list expressions like "1,3,5"
func (s *Sanitizer) validateCronList(field string, minVal, maxVal int, fieldName string) error {
	values := strings.Split(field, ",")
	if len(values) > 10 { // Prevent excessively long lists
		return fmt.Errorf("too many values in %s field list (maximum 10)", fieldName)
	}

	for _, val := range values {
		val = strings.TrimSpace(val)
		if val == "" {
			return fmt.Errorf("empty value in %s field list", fieldName)
		}
		
		intVal, err := strconv.Atoi(val)
		if err != nil || intVal < minVal || intVal > maxVal {
			return fmt.Errorf("invalid value %s in %s field list (must be %d-%d)", val, fieldName, minVal, maxVal)
		}
	}
	return nil
}

// SanitizeHTML performs HTML escaping to prevent XSS
func (s *Sanitizer) SanitizeHTML(input string) string {
	return html.EscapeString(input)
}

// ValidateJobName validates job names with enhanced security
func (s *Sanitizer) ValidateJobName(name string) error {
	// Check length
	if len(name) == 0 || len(name) > 100 {
		return fmt.Errorf("job name must be between 1 and 100 characters")
	}

	// Allow only alphanumeric, dash, underscore, and dots
	if !regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`).MatchString(name) {
		return fmt.Errorf("job name can only contain letters, numbers, dashes, underscores, and dots")
	}

	// Prevent names that could cause confusion or security issues
	reservedNames := []string{
		".", "..", "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
		"root", "admin", "administrator", "system", "daemon", "bin", "sys",
	}

	upperName := strings.ToUpper(name)
	for _, reserved := range reservedNames {
		if upperName == strings.ToUpper(reserved) {
			return fmt.Errorf("job name '%s' is reserved and not allowed", name)
		}
	}

	return nil
}

// ValidateEmailList validates a comma-separated list of email addresses
func (s *Sanitizer) ValidateEmailList(emails string) error {
	if emails == "" {
		return nil
	}

	emailList := strings.Split(emails, ",")
	if len(emailList) > 20 { // Prevent excessive email lists
		return fmt.Errorf("too many email addresses (maximum 20)")
	}

	// Enhanced email regex for better validation
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

	for _, email := range emailList {
		email = strings.TrimSpace(email)
		if email == "" {
			return fmt.Errorf("empty email address in list")
		}
		
		if len(email) > 254 { // RFC 5321 limit
			return fmt.Errorf("email address too long: %s", email)
		}
		
		if !emailRegex.MatchString(email) {
			return fmt.Errorf("invalid email address format: %s", email)
		}

		// Additional security checks
		if strings.Contains(email, "..") {
			return fmt.Errorf("email address contains consecutive dots: %s", email)
		}
	}

	return nil
}