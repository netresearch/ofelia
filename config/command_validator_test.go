package config

import (
	"strings"
	"testing"
)

func TestNewCommandValidator(t *testing.T) {
	v := NewCommandValidator()
	if v == nil {
		t.Fatal("NewCommandValidator returned nil")
	}
	if v.serviceNamePattern == nil {
		t.Error("serviceNamePattern not initialized")
	}
	if v.filePathPattern == nil {
		t.Error("filePathPattern not initialized")
	}
	if len(v.dangerousPatterns) == 0 {
		t.Error("dangerousPatterns not initialized")
	}
}

func TestValidateServiceName(t *testing.T) {
	v := NewCommandValidator()

	tests := []struct {
		name      string
		service   string
		wantError bool
		errorMsg  string
	}{
		// Valid cases
		{"valid simple name", "nginx", false, ""},
		{"valid with underscore", "web_server", false, ""},
		{"valid with hyphen", "web-server", false, ""},
		{"valid with dot", "app.service", false, ""},
		{"valid alphanumeric", "service123", false, ""},
		{"valid mixed", "Web_Server-2.0", false, ""},

		// Invalid cases
		{"empty name", "", true, "empty"},
		{"with space", "web server", true, "invalid characters"},
		{"with semicolon", "web;server", true, "dangerous pattern"},
		{"with pipe", "web|server", true, "dangerous pattern"},
		{"with ampersand", "web&server", true, "dangerous pattern"},
		{"with redirect", "web>server", true, "dangerous pattern"},
		{"with backtick", "web`server`", true, "dangerous pattern"},
		{"with command substitution", "$(whoami)", true, "dangerous pattern"},
		{"too long", strings.Repeat("a", 256), true, "too long"},
		{"with null byte", "web\x00server", true, "dangerous pattern"},
		{"with directory traversal", "../../../etc", true, "dangerous pattern"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateServiceName(tt.service)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateServiceName(%q) error = %v, wantError %v", tt.service, err, tt.wantError)
			}
			if err != nil && tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("ValidateServiceName(%q) error = %v, should contain %q", tt.service, err, tt.errorMsg)
			}
		})
	}
}

func TestValidateFilePath(t *testing.T) {
	v := NewCommandValidator()

	tests := []struct {
		name      string
		path      string
		wantError bool
		errorMsg  string
	}{
		// Valid cases
		{"valid simple file", "docker-compose.yml", false, ""},
		{"valid with path", "configs/docker-compose.yml", false, ""},
		{"valid with dot", "./docker-compose.yml", false, ""},
		{"valid nested", "path/to/file.yml", false, ""},
		{"valid with underscore", "docker_compose.yml", false, ""},
		{"valid with hyphen", "docker-compose-prod.yml", false, ""},

		// Invalid cases
		{"empty path", "", true, "empty"},
		{"with space", "docker compose.yml", true, "invalid characters"},
		{"with semicolon", "file;rm -rf", true, "dangerous pattern"},
		{"with pipe", "file|cat", true, "dangerous pattern"},
		{"with redirect", "file>output", true, "dangerous pattern"},
		{"with backtick", "file`cmd`", true, "dangerous pattern"},
		{"system directory etc", "/etc/passwd", true, "sensitive"},
		{"system directory proc", "/proc/self/environ", true, "sensitive"},
		{"system directory sys", "/sys/power/state", true, "sensitive directory"},
		{"system directory dev", "/dev/null", true, "sensitive directory"},
		{"directory traversal", "../../../../../../etc/passwd", true, "dangerous pattern"},
		{"too long", strings.Repeat("a", 4097), true, "too long"},
		{"with null byte", "file\x00.yml", true, "dangerous pattern"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateFilePath(tt.path)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateFilePath(%q) error = %v, wantError %v", tt.path, err, tt.wantError)
			}
			if err != nil && tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("ValidateFilePath(%q) error = %v, should contain %q", tt.path, err, tt.errorMsg)
			}
		})
	}
}

func TestValidateCommandArgs(t *testing.T) {
	v := NewCommandValidator()

	tests := []struct {
		name      string
		args      []string
		wantError bool
		errorMsg  string
	}{
		// Valid cases
		{"valid simple args", []string{"echo", "hello", "world"}, false, ""},
		{"valid with flags", []string{"--verbose", "--output", "file.txt"}, false, ""},
		{"valid with equals", []string{"--key=value", "--flag"}, false, ""},
		{"valid paths", []string{"/app/script.sh", "./relative/path"}, false, ""},

		// Invalid cases
		{"with command substitution", []string{"echo", "$(whoami)"}, true, "dangerous pattern"},
		{"with backtick", []string{"echo", "`id`"}, true, "dangerous pattern"},
		{"with pipe", []string{"echo", "test", "|", "grep", "test"}, true, "dangerous pattern"},
		{"with semicolon", []string{"echo", "test;", "rm", "-rf"}, true, "dangerous pattern"},
		{"with ampersand", []string{"echo", "test", "&"}, true, "dangerous pattern"},
		{"with redirect out", []string{"echo", "test", ">", "/etc/passwd"}, true, "dangerous pattern"},
		{"with redirect in", []string{"cat", "<", "/etc/passwd"}, true, "dangerous pattern"},
		{"with null byte", []string{"echo", "test\x00value"}, true, "dangerous pattern"},
		{"too long arg", []string{strings.Repeat("a", 4097)}, true, "too long"},
		{"directory traversal", []string{"cat", "../../../etc/passwd"}, true, "dangerous pattern"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateCommandArgs(tt.args)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateCommandArgs(%v) error = %v, wantError %v", tt.args, err, tt.wantError)
			}
			if err != nil && tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("ValidateCommandArgs(%v) error = %v, should contain %q", tt.args, err, tt.errorMsg)
			}
		})
	}
}

func TestSanitizeCommand(t *testing.T) {
	v := NewCommandValidator()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal command", "echo hello", "echo hello"},
		{"with null byte", "echo\x00hello", "echohello"},
		{"too long", strings.Repeat("a", 5000), strings.Repeat("a", 4096)},
		{"multiple null bytes", "e\x00c\x00h\x00o", "echo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v.SanitizeCommand(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeCommand(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidatorSecurityPatterns(t *testing.T) {
	v := NewCommandValidator()

	// Test that all dangerous patterns are properly detected
	dangerousInputs := []struct {
		name    string
		input   string
		pattern string
	}{
		{"command substitution dollar", "$(whoami)", "command substitution"},
		{"command substitution backtick", "`id`", "backtick"},
		{"pipe operator", "test | grep", "pipe"},
		{"semicolon separator", "test; ls", "semicolon"},
		{"background operator", "test &", "ampersand"},
		{"and operator", "test && ls", "ampersand"},
		{"output redirect", "test > file", "redirect"},
		{"input redirect", "test < file", "redirect"},
		{"directory traversal", "../../etc/passwd", "traversal"},
		{"null byte", "test\x00value", "null"},
	}

	for _, tt := range dangerousInputs {
		t.Run(tt.name, func(t *testing.T) {
			// Test in service name
			err := v.ValidateServiceName(tt.input)
			if err == nil {
				t.Errorf("ValidateServiceName should reject %s pattern in %q", tt.pattern, tt.input)
			}

			// Test in file path (if not a system path)
			if !strings.HasPrefix(tt.input, "/") {
				err = v.ValidateFilePath(tt.input)
				if err == nil {
					t.Errorf("ValidateFilePath should reject %s pattern in %q", tt.pattern, tt.input)
				}
			}

			// Test in command args
			err = v.ValidateCommandArgs([]string{tt.input})
			if err == nil {
				t.Errorf("ValidateCommandArgs should reject %s pattern in %q", tt.pattern, tt.input)
			}
		})
	}
}

func BenchmarkValidateServiceName(b *testing.B) {
	v := NewCommandValidator()
	service := "web-server_123.service"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.ValidateServiceName(service)
	}
}

func BenchmarkValidateFilePath(b *testing.B) {
	v := NewCommandValidator()
	path := "configs/docker-compose.yml"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.ValidateFilePath(path)
	}
}

func BenchmarkValidateCommandArgs(b *testing.B) {
	v := NewCommandValidator()
	args := []string{"echo", "hello", "world", "--verbose"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.ValidateCommandArgs(args)
	}
}
