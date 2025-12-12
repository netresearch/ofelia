package middlewares

import (
	"fmt"
	"regexp"
	"strings"
)

const defaultBranch = "main"

// GitHubShorthand represents a parsed GitHub shorthand URL
type GitHubShorthand struct {
	Org     string
	Repo    string
	Path    string
	Version string // Can be tag, branch, or commit
}

// githubShorthandRegex matches patterns like:
// - gh:org/repo/path/to/preset.yaml
// - gh:org/repo/path/to/preset.yaml@v1.0.0
// - gh:org/repo/path/to/preset.yaml@main
// - gh:org/repo/path/to/preset@v1.0  (auto-adds .yaml)
var githubShorthandRegex = regexp.MustCompile(`^gh:([^/]+)/([^/@]+)(/[^@]*)?(?:@(.+))?$`)

// ParseGitHubShorthand parses a GitHub shorthand URL and returns the raw URL
// Format: gh:org/repo/path/to/file.yaml@version
// Examples:
//   - gh:netresearch/ofelia-presets/slack.yaml
//   - gh:netresearch/ofelia-presets/notifications/slack.yaml@v1.0.0
//   - gh:myorg/my-presets/custom@main
func ParseGitHubShorthand(shorthand string) (string, error) {
	if !strings.HasPrefix(shorthand, "gh:") {
		return "", fmt.Errorf("not a GitHub shorthand (must start with gh:)")
	}

	matches := githubShorthandRegex.FindStringSubmatch(shorthand)
	if matches == nil {
		return "", fmt.Errorf("invalid GitHub shorthand format: %s (expected gh:org/repo/path[@version])", shorthand)
	}

	gh := GitHubShorthand{
		Org:  matches[1],
		Repo: matches[2],
	}

	// Path (optional, may include leading /)
	if matches[3] != "" {
		gh.Path = strings.TrimPrefix(matches[3], "/")
	}

	// Version (optional)
	if matches[4] != "" {
		gh.Version = matches[4]
	} else {
		gh.Version = defaultBranch // Default to main branch
	}

	// Ensure path ends with .yaml
	if gh.Path != "" && !strings.HasSuffix(gh.Path, ".yaml") && !strings.HasSuffix(gh.Path, ".yml") {
		gh.Path += ".yaml"
	}

	// Build raw GitHub URL
	// Format: https://raw.githubusercontent.com/org/repo/version/path
	var url string
	if gh.Path != "" {
		url = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
			gh.Org, gh.Repo, gh.Version, gh.Path)
	} else {
		// If no path, assume preset.yaml at root
		url = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/preset.yaml",
			gh.Org, gh.Repo, gh.Version)
	}

	return url, nil
}

// ParseGitHubShorthandDetails parses and returns the structured details
func ParseGitHubShorthandDetails(shorthand string) (*GitHubShorthand, error) {
	if !strings.HasPrefix(shorthand, "gh:") {
		return nil, fmt.Errorf("not a GitHub shorthand")
	}

	matches := githubShorthandRegex.FindStringSubmatch(shorthand)
	if matches == nil {
		return nil, fmt.Errorf("invalid GitHub shorthand format")
	}

	gh := &GitHubShorthand{
		Org:  matches[1],
		Repo: matches[2],
	}

	if matches[3] != "" {
		gh.Path = strings.TrimPrefix(matches[3], "/")
	}

	if matches[4] != "" {
		gh.Version = matches[4]
	} else {
		gh.Version = defaultBranch
	}

	return gh, nil
}

// IsGitHubShorthand checks if a string is a GitHub shorthand
func IsGitHubShorthand(s string) bool {
	return strings.HasPrefix(s, "gh:")
}

// IsVersioned checks if the shorthand includes an explicit version
func IsVersioned(shorthand string) bool {
	return strings.Contains(shorthand, "@")
}

// FormatGitHubShorthand creates a shorthand string from components
func FormatGitHubShorthand(org, repo, path, version string) string {
	var sb strings.Builder
	sb.WriteString("gh:")
	sb.WriteString(org)
	sb.WriteString("/")
	sb.WriteString(repo)

	if path != "" {
		if !strings.HasPrefix(path, "/") {
			sb.WriteString("/")
		}
		sb.WriteString(path)
	}

	if version != "" && version != defaultBranch {
		sb.WriteString("@")
		sb.WriteString(version)
	}

	return sb.String()
}

// ValidateGitHubShorthand validates that a GitHub shorthand is well-formed
func ValidateGitHubShorthand(shorthand string) error {
	if !IsGitHubShorthand(shorthand) {
		return fmt.Errorf("not a GitHub shorthand")
	}

	_, err := ParseGitHubShorthand(shorthand)
	return err
}

// ExtractVersionFromShorthand extracts the version from a shorthand
func ExtractVersionFromShorthand(shorthand string) string {
	idx := strings.LastIndex(shorthand, "@")
	if idx == -1 {
		return ""
	}
	return shorthand[idx+1:]
}

// StripVersionFromShorthand removes the version from a shorthand
func StripVersionFromShorthand(shorthand string) string {
	idx := strings.LastIndex(shorthand, "@")
	if idx == -1 {
		return shorthand
	}
	return shorthand[:idx]
}

// IsSemanticVersion checks if a version string looks like a semantic version
func IsSemanticVersion(version string) bool {
	// Simple check for v-prefixed or digit-starting versions
	version = strings.TrimPrefix(version, "v")
	if len(version) == 0 {
		return false
	}
	return version[0] >= '0' && version[0] <= '9'
}

// IsBranch attempts to determine if a version is a branch name
func IsBranch(version string) bool {
	// Common branch patterns
	branches := []string{"main", "master", "develop", "dev", "staging", "production"}
	for _, b := range branches {
		if version == b {
			return true
		}
	}

	// Feature branch pattern
	if strings.HasPrefix(version, "feature/") ||
		strings.HasPrefix(version, "fix/") ||
		strings.HasPrefix(version, "release/") {
		return true
	}

	return false
}
