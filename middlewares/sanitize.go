package middlewares

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// PathSanitizer provides secure path sanitization utilities
type PathSanitizer struct {
	// Patterns that could indicate path traversal attempts
	dangerousPatterns []*regexp.Regexp
	// Characters to replace in filenames
	replacer *strings.Replacer
}

// NewPathSanitizer creates a new path sanitizer with security rules
func NewPathSanitizer() *PathSanitizer {
	return &PathSanitizer{
		dangerousPatterns: []*regexp.Regexp{
			regexp.MustCompile(`\.\.`),                    // Directory traversal
			regexp.MustCompile(`^~`),                      // Home directory reference
			regexp.MustCompile(`(?i)^(con|prn|aux|nul)`), // Windows reserved names
			regexp.MustCompile(`[<>:"|?*]`),               // Invalid filename chars
		},
		replacer: strings.NewReplacer(
			"/", "_",
			"\\", "_",
			"..", "_",
			"~", "_",
			"$", "_",
			"`", "_",
			"|", "_",
			"<", "_",
			">", "_",
			":", "_",
			"\"", "_",
			"?", "_",
			"*", "_",
			"\x00", "_", // Null byte
		),
	}
}

// SanitizePath sanitizes a path to prevent directory traversal and injection
func (ps *PathSanitizer) SanitizePath(path string) string {
	// First apply replacements to handle null bytes and other dangerous chars
	cleaned := ps.replacer.Replace(path)
	
	// Then clean the path to resolve any . or .. elements
	cleaned = filepath.Clean(cleaned)
	
	// Check for dangerous patterns
	for _, pattern := range ps.dangerousPatterns {
		if pattern.MatchString(cleaned) {
			// If dangerous pattern found, sanitize more aggressively
			cleaned = ps.replacer.Replace(cleaned)
			break
		}
	}
	
	// Ensure the path doesn't start with / or drive letter on Windows
	// to prevent absolute path injection
	if filepath.IsAbs(cleaned) {
		// Convert to relative path by removing leading separators
		cleaned = strings.TrimLeft(cleaned, "/\\")
		// Also remove Windows drive letters
		if len(cleaned) > 1 && cleaned[1] == ':' {
			cleaned = cleaned[2:]
			cleaned = strings.TrimLeft(cleaned, "/\\")
		}
	}
	
	return cleaned
}

// SanitizeFilename sanitizes a filename for safe file system operations
func (ps *PathSanitizer) SanitizeFilename(filename string) string {
	// Apply replacements for dangerous characters
	safe := ps.replacer.Replace(filename)
	
	// Limit filename length to prevent issues
	const maxLength = 255
	if len(safe) > maxLength {
		// Preserve extension if possible
		ext := filepath.Ext(safe)
		if len(ext) < maxLength {
			safe = safe[:maxLength-len(ext)] + ext
		} else {
			safe = safe[:maxLength]
		}
	}
	
	// Ensure filename is not empty after sanitization
	if safe == "" || safe == "." {
		safe = "unnamed"
	}
	
	return safe
}

// SanitizeJobName sanitizes a job name for use in filenames
func (ps *PathSanitizer) SanitizeJobName(jobName string) string {
	// Job names might contain special characters that are invalid in filenames
	return ps.SanitizeFilename(jobName)
}

// ValidateSaveFolder validates that a save folder path is safe to use
func (ps *PathSanitizer) ValidateSaveFolder(folder string) error {
	// Check if folder path contains dangerous patterns
	for _, pattern := range ps.dangerousPatterns {
		if pattern.MatchString(folder) {
			return fmt.Errorf("invalid save folder path: contains dangerous pattern")
		}
	}
	
	// Ensure it's not trying to write to system directories
	cleanPath := filepath.Clean(folder)
	systemDirs := []string{"/etc", "/bin", "/sbin", "/usr/bin", "/usr/sbin", "/sys", "/proc", "/dev"}
	for _, sysDir := range systemDirs {
		if strings.HasPrefix(cleanPath, sysDir) {
			return fmt.Errorf("invalid save folder: cannot write to system directory %s", sysDir)
		}
	}
	
	return nil
}

// Default sanitizer instance
var DefaultSanitizer = NewPathSanitizer()

// SanitizePath is a convenience function using the default sanitizer
func SanitizePath(path string) string {
	return DefaultSanitizer.SanitizePath(path)
}

// SanitizeFilename is a convenience function using the default sanitizer
func SanitizeFilename(filename string) string {
	return DefaultSanitizer.SanitizeFilename(filename)
}

// SanitizeJobName is a convenience function using the default sanitizer
func SanitizeJobName(jobName string) string {
	return DefaultSanitizer.SanitizeJobName(jobName)
}