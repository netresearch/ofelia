package middlewares

import (
	"testing"
)

func TestPathSanitizer_SanitizePath(t *testing.T) {
	ps := NewPathSanitizer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Normal path", "logs/app.log", "logs_app.log"},
		{"Directory traversal", "../../../etc/passwd", "______etc_passwd"},
		{"Absolute path", "/etc/passwd", "_etc_passwd"},
		{"Windows absolute", "C:\\Windows\\System32", "C__Windows_System32"},
		{"Home directory", "~/secrets", "__secrets"},
		{"Null bytes", "file\x00.txt", "file_.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ps.SanitizePath(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizePath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPathSanitizer_SanitizeFilename(t *testing.T) {
	ps := NewPathSanitizer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Normal filename", "report.pdf", "report.pdf"},
		{"Special chars", "file<>:\"|?*.txt", "file_______.txt"},
		{"Empty after sanitization", "...", "_."},
		{"Very long filename", string(make([]byte, 300)), string(make([]byte, 255))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ps.SanitizeFilename(tt.input)
			if len(result) > 255 {
				t.Errorf("SanitizeFilename result too long: %d chars", len(result))
			}
			if tt.input != string(make([]byte, 300)) && result != tt.expected {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPathSanitizer_ValidateSaveFolder(t *testing.T) {
	ps := NewPathSanitizer()

	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{"Normal folder", "/var/log/ofelia", false},
		{"Relative folder", "logs/ofelia", false},
		{"System folder", "/etc/ofelia", true},
		{"Binary folder", "/usr/bin", true},
		{"Directory traversal", "../../../etc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ps.ValidateSaveFolder(tt.input)
			hasError := err != nil
			if hasError != tt.wantError {
				t.Errorf("ValidateSaveFolder(%q) error = %v, wantError = %v", tt.input, err, tt.wantError)
			}
		})
	}
}

func TestSanitizeJobName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Normal job name", "backup-db", "backup-db"},
		{"Job with slashes", "backup/database", "backup_database"},
		{"Job with path traversal", "../../../admin", "______admin"},
		{"Job with special chars", "job:daily|backup", "job_daily_backup"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeJobName(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeJobName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
