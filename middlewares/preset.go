// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

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
	// httpClient is the cached HTTP client used for remote preset fetches
	// (loadFromURL / loadFromGitHub). It is constructed once in
	// NewPresetLoader so multiple fetches share a connection pool / idle-conn
	// reuse instead of paying the cost of a fresh *http.Transport per call.
	//
	// Test ordering constraint: tests that override the transport factory via
	// SetTransportFactoryForTest MUST do so BEFORE calling NewPresetLoader —
	// the override has no effect on a client that has already been cached.
	httpClient *http.Client
}

// NewPresetLoader creates a new preset loader.
//
// The HTTP client used for remote preset fetches (loadFromURL,
// loadFromGitHub) is constructed here from TransportFactory() and cached on
// the returned loader. Callers that need to influence the transport (e.g.
// tests using SetTransportFactoryForTest) must install the override BEFORE
// calling NewPresetLoader; replacing the factory afterwards does not affect
// the already-cached client.
func NewPresetLoader(globalConfig *WebhookGlobalConfig) *PresetLoader {
	loader := &PresetLoader{
		bundledPresets: make(map[string]*Preset),
		globalConfig:   globalConfig,
		// Cache an HTTP client routed through the shared webhook
		// TransportFactory so the TLS / proxy posture is centrally configured
		// and consistent with webhook delivery. Sharing a single client
		// enables connection pooling across bursty preset fetches.
		// Per-request deadlines are set via context.WithTimeout in
		// loadFromURL, so we deliberately do not set Client.Timeout here.
		httpClient: &http.Client{Transport: TransportFactory()},
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

// AddLocalPresetDir adds a directory to search for local preset files. It
// also scans the directory for files whose names collide with a bundled
// preset (e.g. "json-post.yaml" shadowing the bundled "json-post") and emits
// a startup-time slog.Warn per collision. Load() resolves bundled presets
// first and never falls through, so a local file at $dir/<name>.yaml meant
// to override the bundled preset would silently be ignored — surfacing the
// shadow at AddLocalPresetDir time turns a silent no-op into an actionable
// warning. See https://github.com/netresearch/ofelia/issues/679.
//
// Inverting the lookup order to prefer local files is deliberately rejected:
// a local typo that accidentally shadows "slack" would silently break Slack
// delivery for every webhook on the host. Operators trust the bundled set
// to behave consistently across hosts; warn-and-keep-bundled preserves that.
func (l *PresetLoader) AddLocalPresetDir(dir string) {
	l.localPresetDirs = append(l.localPresetDirs, dir)
	l.warnOnBundledShadow(dir)
}

// warnOnBundledShadow scans dir for *.yaml files whose stem matches a
// bundled preset name and emits one slog.Warn per collision. Restricted to
// the *.yaml extension on purpose: Load() only probes "<name>.yaml" in
// localPresetDirs, so a *.yml file would not have been loaded anyway and
// warning on it would mislead operators into thinking the rename to .yaml
// is sufficient.
//
// A non-existent directory (operator registered before populating) is
// silent. Other read failures are debug-logged so a permission-denied or
// I/O error is still discoverable when an operator turns up the log level,
// without becoming a startup-noise floor on a clean install.
func (l *PresetLoader) warnOnBundledShadow(dir string) {
	if len(l.bundledPresets) == 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Default().Debug(
				"PresetLoader: could not scan local preset dir for bundled-name shadows",
				"dir", dir,
				"error", err,
			)
		}
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".yaml" {
			continue
		}
		stem := strings.TrimSuffix(name, ".yaml")
		if _, shadowed := l.bundledPresets[stem]; !shadowed {
			continue
		}
		fullPath := filepath.Join(dir, name)
		slog.Default().Warn(
			fmt.Sprintf("Local preset file %q shadows bundled preset %q; the bundled preset wins "+
				"and the local file will not be loaded. Rename the file or remove it to silence this warning.",
				fullPath, stem),
			"file", fullPath,
			"bundled", stem,
		)
	}
}

// DefaultPreset returns the effective global default preset name —
// (*WebhookGlobalConfig).EffectiveDefaultPreset() unwrapped, with a nil
// globalConfig also resolving to the bundled DefaultPresetName so tests
// that construct a loader without a global config still get the fallback.
// Callers fill this into WebhookConfig.Preset when the per-webhook value
// is empty, so url-only webhooks work without each one redeclaring `preset`.
//
// Operators can opt out of the fallback by setting `webhook-default-preset`
// to an empty string in INI or via Docker label; that path returns "" here,
// and NewWebhook then fails attachment for any webhook that omits `preset`
// — regardless of whether `url` is set — with an error naming
// webhook-default-preset so operators can grep their way to the docs.
// (Setting `url` alone is not enough once the fallback is disabled: `url`
// only overrides the preset's url_scheme, not the preset itself.)
//
// See https://github.com/netresearch/ofelia/issues/676.
func (l *PresetLoader) DefaultPreset() string {
	if l.globalConfig == nil {
		return DefaultPresetName
	}
	return l.globalConfig.EffectiveDefaultPreset()
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

	// Use the cached *http.Client constructed in NewPresetLoader so bursty
	// preset fetches reuse the underlying connection pool. The transport was
	// produced by TransportFactory() at construction time so the TLS / proxy
	// posture stays consistent with webhook delivery. The request-level
	// deadline is already set by the context above, so we don't add a
	// Client.Timeout.
	resp, err := l.httpClient.Do(req)
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
			preset.Headers["Content-Type"] = contentTypeJSON
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
	// json escapes the *interior* of a JSON string literal. Callers
	// typically wrap the result in `"..."` themselves, e.g.
	//   "text": "{{json .Job.Command}}"
	//
	// The escape table follows RFC 8259 section 7 — `"`, `\`, every byte
	// in U+0000-U+001F. The earlier hand-rolled version escaped only
	// `\`, `"`, `\n`, `\r`, `\t`, leaving NUL, BEL, FF, terminal-escape
	// `\x1b`, and the other control codes to slip through unescaped.
	// Command output legitimately contains these (progress bars, ANSI
	// color, binary tools), and a strict JSON parser on the receiving
	// webhook endpoint would reject the body. No injection vector
	// existed — `"` and `\` were always escaped — but availability
	// (notifications lost) was at risk. See #676 review.
	"json": jsonStringEscape,

	// jsonRaw marshals an arbitrary template value into a self-contained
	// JSON literal (including surrounding quotes for strings, or `true`/
	// `false`/`null`/numbers/arrays/objects as appropriate). Use when a
	// template wants to emit a fully-formed JSON value rather than
	// inserting the inner of a string literal — i.e. opposite contract
	// from `json`. Errors collapse to `"<jsonRaw error: ...>"` so a
	// template failure is visible at the receiver instead of silently
	// producing invalid JSON.
	"jsonRaw": jsonRawMarshal,
	// truncate cuts s to at most n runes, appending "..." when truncation
	// happened. Rune-aware (not byte-aware) so multi-byte UTF-8 sequences
	// (terminal escapes, emoji, non-ASCII command output) are never split
	// mid-rune. A byte slice that ends inside a UTF-8 sequence would
	// produce a replacement rune downstream and almost certainly break
	// the receiver's JSON parser when paired with the `json` escape.
	"truncate": func(n int, s string) string {
		if utf8.RuneCountInString(s) <= n {
			return s
		}
		// Walk n runes, return the byte slice up to that boundary.
		count := 0
		for i := range s {
			if count == n {
				return s[:i] + "..."
			}
			count++
		}
		return s + "..." // unreachable: rune count > n above
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

// jsonStringEscape returns s escaped for embedding inside a JSON string
// literal (without the surrounding quotes). Covers RFC 8259 section 7:
//
//   - U+0022 `"` and U+005C `\` use their short escape forms.
//   - U+0008 / U+000C / U+000A / U+000D / U+0009 use `\b` / `\f` / `\n` /
//     `\r` / `\t`.
//   - All other U+0000-U+001F use `\u00XX`.
//
// Bytes ≥ U+0020 (including valid multi-byte UTF-8) pass through. JSON
// allows U+007F-U+FFFF unescaped in strings; receivers handle them.
//
// Implementation note: text/template is not stdlib JSON encoder, so we
// can't lean on encoding/json.Marshal here without rewriting every
// bundled preset to drop its outer `"..."` wrappers. Keeping the
// "interior of a string literal" contract preserves consistency with
// slack.yaml / discord.yaml / etc.
func jsonStringEscape(s string) string {
	// Fast path: pure ASCII-printable input needs no escaping. Hot for
	// short Job.Name / Schedule strings.
	if !needsJSONEscape(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := range len(s) {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, c)
				continue
			}
			b.WriteByte(c)
		}
	}
	return b.String()
}

func needsJSONEscape(s string) bool {
	for i := range len(s) {
		c := s[i]
		if c == '"' || c == '\\' || c < 0x20 {
			return true
		}
	}
	return false
}

// jsonRawMarshal marshals v through encoding/json so a template can emit
// a fully-formed JSON value (string-with-quotes, number, bool, etc.)
// rather than just the interior of a string literal. Marshal errors are
// surfaced as a visible string in the body so a receiver-side JSON parse
// fails loudly instead of silently producing malformed output.
func jsonRawMarshal(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%q", "<jsonRaw error: "+err.Error()+">")
	}
	return string(raw)
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
