package middlewares

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

//go:embed presets/*.yaml
var embeddedPresets embed.FS

// PresetVariable defines a variable that can be used in the preset
type PresetVariable struct {
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Sensitive   bool   `yaml:"sensitive"`
	Default     string `yaml:"default,omitempty"`
	Example     string `yaml:"example,omitempty"`
}

// Preset defines a webhook notification preset
type Preset struct {
	// Metadata
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`

	// URL configuration
	URLScheme string `yaml:"url_scheme"` //nolint:tagliatelle // snake_case is idiomatic for YAML configs

	// HTTP configuration
	Method  string            `yaml:"method"`
	Headers map[string]string `yaml:"headers"`

	// Variable definitions
	Variables map[string]PresetVariable `yaml:"variables"`

	// Body template (Go text/template format)
	Body string `yaml:"body"`
}

// PresetLoader handles loading presets from various sources
type PresetLoader struct {
	bundledPresets  map[string]*Preset
	localPresetDirs []string
	cache           *PresetCache
	globalConfig    *WebhookGlobalConfig
}

// NewPresetLoader creates a new preset loader
func NewPresetLoader(globalConfig *WebhookGlobalConfig) *PresetLoader {
	loader := &PresetLoader{
		bundledPresets: make(map[string]*Preset),
		globalConfig:   globalConfig,
	}

	// Load all bundled presets
	if err := loader.loadBundledPresets(); err != nil {
		// Log but don't fail - bundled presets are optional
		fmt.Fprintf(os.Stderr, "Warning: failed to load bundled presets: %v\n", err)
	}

	// Initialize cache if remote presets are allowed
	if globalConfig != nil && globalConfig.AllowRemotePresets {
		loader.cache = NewPresetCache(globalConfig.PresetCacheDir, globalConfig.PresetCacheTTL)
	}

	return loader
}

// loadBundledPresets loads all presets embedded in the binary
func (l *PresetLoader) loadBundledPresets() error {
	entries, err := embeddedPresets.ReadDir("presets")
	if err != nil {
		return fmt.Errorf("read embedded presets dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		data, err := embeddedPresets.ReadFile("presets/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read embedded preset %s: %w", entry.Name(), err)
		}

		preset, err := ParsePreset(data)
		if err != nil {
			return fmt.Errorf("parse embedded preset %s: %w", entry.Name(), err)
		}

		// Use preset name as key, fallback to filename without extension
		key := preset.Name
		if key == "" {
			key = strings.TrimSuffix(entry.Name(), ".yaml")
		}
		l.bundledPresets[key] = preset
	}

	return nil
}

// AddLocalPresetDir adds a directory to search for local preset files
func (l *PresetLoader) AddLocalPresetDir(dir string) {
	l.localPresetDirs = append(l.localPresetDirs, dir)
}

// Load loads a preset by name or path
// Supports:
// - Built-in preset names: "slack", "discord", etc.
// - Local file paths: "/path/to/preset.yaml", "./preset.yaml"
// - GitHub shorthand: "gh:org/repo/path/preset.yaml@v1.0"
// - Full URLs: "https://example.com/preset.yaml"
func (l *PresetLoader) Load(presetSpec string) (*Preset, error) {
	// Check for empty spec
	if presetSpec == "" {
		return nil, fmt.Errorf("preset specification cannot be empty")
	}

	// 1. Check bundled presets first
	if preset, ok := l.bundledPresets[presetSpec]; ok {
		return preset, nil
	}

	// 2. Check local file path
	if strings.HasPrefix(presetSpec, "/") || strings.HasPrefix(presetSpec, "./") || strings.HasPrefix(presetSpec, "../") {
		return l.loadFromFile(presetSpec)
	}

	// 3. Check local preset directories
	for _, dir := range l.localPresetDirs {
		path := filepath.Join(dir, presetSpec+".yaml")
		if _, err := os.Stat(path); err == nil {
			return l.loadFromFile(path)
		}
	}

	// 4. Check for GitHub shorthand
	if strings.HasPrefix(presetSpec, "gh:") {
		return l.loadFromGitHub(presetSpec)
	}

	// 5. Check for full URL
	if strings.HasPrefix(presetSpec, "http://") || strings.HasPrefix(presetSpec, "https://") {
		return l.loadFromURL(presetSpec)
	}

	// Not found anywhere
	return nil, fmt.Errorf("preset %q not found (checked: bundled, local dirs, github, url)", presetSpec)
}

// loadFromFile loads a preset from a local file
func (l *PresetLoader) loadFromFile(path string) (*Preset, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- intentional user-configured path
	if err != nil {
		return nil, fmt.Errorf("read preset file %s: %w", path, err)
	}

	preset, err := ParsePreset(data)
	if err != nil {
		return nil, fmt.Errorf("parse preset file %s: %w", path, err)
	}

	return preset, nil
}

// loadFromGitHub loads a preset from GitHub using shorthand notation
func (l *PresetLoader) loadFromGitHub(spec string) (*Preset, error) {
	if l.globalConfig == nil || !l.globalConfig.AllowRemotePresets {
		return nil, fmt.Errorf("remote presets are disabled (set allow-remote-presets = true)")
	}

	url, err := ParseGitHubShorthand(spec)
	if err != nil {
		return nil, err
	}

	// Check trusted sources
	if !l.isTrustedSource(spec) {
		return nil, fmt.Errorf("preset source %q is not in trusted-preset-sources", spec)
	}

	return l.loadFromURL(url)
}

// loadFromURL loads a preset from a remote URL
func (l *PresetLoader) loadFromURL(url string) (*Preset, error) {
	if l.globalConfig == nil || !l.globalConfig.AllowRemotePresets {
		return nil, fmt.Errorf("remote presets are disabled (set allow-remote-presets = true)")
	}

	// Check cache first
	if l.cache != nil {
		if preset, err := l.cache.Get(url); err == nil {
			return preset, nil
		}
	}

	// Validate URL for SSRF protection
	if err := ValidateWebhookURL(url); err != nil {
		return nil, fmt.Errorf("preset URL validation failed: %w", err)
	}

	// Fetch from remote with context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch preset from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch preset from %s: HTTP %d", url, resp.StatusCode)
	}

	// Read body with size limit (1MB)
	const maxSize = 1024 * 1024
	data := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
			if len(data) > maxSize {
				return nil, fmt.Errorf("preset file too large (max %d bytes)", maxSize)
			}
		}
		if err != nil {
			break
		}
	}

	preset, err := ParsePreset(data)
	if err != nil {
		return nil, fmt.Errorf("parse preset from %s: %w", url, err)
	}

	// Cache the result
	if l.cache != nil {
		_ = l.cache.Put(url, preset)
	}

	return preset, nil
}

// isTrustedSource checks if a preset source matches the trusted sources pattern
func (l *PresetLoader) isTrustedSource(source string) bool {
	if l.globalConfig == nil || l.globalConfig.TrustedPresetSources == "" {
		return false
	}

	// Parse trusted sources (comma-separated)
	trusted := strings.SplitSeq(l.globalConfig.TrustedPresetSources, ",")
	for pattern := range trusted {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}

		// Simple glob matching
		if matchGlobPattern(pattern, source) {
			return true
		}
	}

	return false
}

// matchGlobPattern performs simple glob matching with * wildcards
func matchGlobPattern(pattern, s string) bool {
	// Simple implementation - supports only * at the end
	if before, ok := strings.CutSuffix(pattern, "*"); ok {
		prefix := before
		return strings.HasPrefix(s, prefix)
	}
	return pattern == s
}

// ParsePreset parses a preset from YAML data
func ParsePreset(data []byte) (*Preset, error) {
	var preset Preset
	if err := yaml.Unmarshal(data, &preset); err != nil {
		return nil, fmt.Errorf("unmarshal preset YAML: %w", err)
	}

	// Validate required fields
	if preset.URLScheme == "" && preset.Body == "" {
		return nil, fmt.Errorf("preset must have either url_scheme or body defined")
	}

	// Set defaults
	if preset.Method == "" {
		preset.Method = http.MethodPost
	}

	if preset.Headers == nil {
		preset.Headers = make(map[string]string)
	}

	// Default Content-Type for POST
	if preset.Method == http.MethodPost {
		if _, ok := preset.Headers["Content-Type"]; !ok {
			preset.Headers["Content-Type"] = "application/json"
		}
	}

	return &preset, nil
}

// BuildURL constructs the final URL by substituting variables
func (p *Preset) BuildURL(config *WebhookConfig) (string, error) {
	// If URL is explicitly set, use it
	if config.URL != "" {
		return config.URL, nil
	}

	// Build URL from scheme with variable substitution
	url := p.URLScheme

	// Replace {id} with config.ID
	url = strings.ReplaceAll(url, "{id}", config.ID)

	// Replace {secret} with config.Secret
	url = strings.ReplaceAll(url, "{secret}", config.Secret)

	// Replace {url} with config.URL (for full URL override pattern)
	if config.URL != "" {
		url = strings.ReplaceAll(url, "{url}", config.URL)
	}

	// Replace any custom variables
	for k, v := range config.CustomVars {
		url = strings.ReplaceAll(url, "{"+k+"}", v)
	}

	// Check for unreplaced variables
	if strings.Contains(url, "{") && strings.Contains(url, "}") {
		return "", fmt.Errorf("URL contains unreplaced variables: %s", url)
	}

	return url, nil
}

// RenderBody renders the body template with the given data
func (p *Preset) RenderBody(data *WebhookData) (string, error) {
	if p.Body == "" {
		return "", nil
	}

	tmpl, err := template.New("body").Funcs(webhookTemplateFuncs).Parse(p.Body)
	if err != nil {
		return "", fmt.Errorf("parse body template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute body template: %w", err)
	}

	return buf.String(), nil
}

// webhookTemplateFuncs provides helper functions for webhook templates
var webhookTemplateFuncs = template.FuncMap{
	"json": func(s string) string {
		// Escape string for JSON embedding
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\"", "\\\"")
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, "\r", "\\r")
		s = strings.ReplaceAll(s, "\t", "\\t")
		return s
	},
	"truncate": func(n int, s string) string {
		if len(s) <= n {
			return s
		}
		return s[:n] + "..."
	},
	"default": func(def, val string) string {
		if val == "" {
			return def
		}
		return val
	},
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
	"title": cases.Title(language.English).String,
	// isoTime formats a time.Time in ISO8601 format
	"isoTime": func(t time.Time) string {
		return t.Format(time.RFC3339)
	},
	// unixTime returns Unix timestamp
	"unixTime": func(t time.Time) int64 {
		return t.Unix()
	},
	// formatDuration formats a duration for display
	"formatDuration": func(d time.Duration) string {
		return d.String()
	},
}

// ListBundledPresets returns the names of all bundled presets
func (l *PresetLoader) ListBundledPresets() []string {
	names := make([]string, 0, len(l.bundledPresets))
	for name := range l.bundledPresets {
		names = append(names, name)
	}
	return names
}

// GetBundledPreset returns a bundled preset by name
func (l *PresetLoader) GetBundledPreset(name string) (*Preset, bool) {
	preset, ok := l.bundledPresets[name]
	return preset, ok
}
