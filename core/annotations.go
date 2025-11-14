package core

import (
	"os"
	"strings"
	"time"
)

// parseAnnotations converts annotation strings in "key=value" format to a map.
// Invalid entries (missing '=' separator) are silently skipped.
func parseAnnotations(annotations []string) map[string]string {
	result := make(map[string]string)
	for _, ann := range annotations {
		parts := strings.SplitN(ann, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key != "" {
				result[key] = value
			}
		}
	}
	return result
}

// getDefaultAnnotations returns default annotations that Ofelia automatically adds.
// User-provided annotations take precedence over these defaults.
func getDefaultAnnotations(jobName, jobType string) map[string]string {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	return map[string]string{
		"ofelia.job.name":       jobName,
		"ofelia.job.type":       jobType,
		"ofelia.execution.time": time.Now().UTC().Format(time.RFC3339),
		"ofelia.scheduler.host": hostname,
		"ofelia.version":        "3.x", // TODO: Extract from build info
	}
}

// mergeAnnotations combines user annotations with default Ofelia annotations.
// User annotations take precedence over defaults (won't be overwritten).
func mergeAnnotations(userAnnotations []string, defaults map[string]string) map[string]string {
	// Start with defaults
	result := make(map[string]string)
	for k, v := range defaults {
		result[k] = v
	}

	// Override with user annotations
	parsed := parseAnnotations(userAnnotations)
	for k, v := range parsed {
		result[k] = v
	}

	return result
}
