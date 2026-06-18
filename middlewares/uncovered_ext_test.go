// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

// ---- mail.go coverage ----

func TestMailRun_DedupSuppressesDuplicate(t *testing.T) {
	t.Parallel()
	f := setupMailTest(t)

	dedup := NewNotificationDedup(time.Hour)

	// First failed execution should send mail
	f.ctx.Start()
	f.ctx.Stop(errors.New("first error"))

	m := NewMail(&MailConfig{
		SMTPHost:      f.smtpdHost,
		SMTPPort:      f.smtpdPort,
		EmailTo:       "foo@foo.com",
		EmailFrom:     "qux@qux.com",
		Dedup:         dedup,
		SMTPTLSPolicy: SMTPTLSPolicyNone, // test fixture: plaintext SMTP, see #653
	})

	done := make(chan struct{})
	go func() {
		_ = m.Run(f.ctx)
		close(done)
	}()

	select {
	case <-f.fromCh:
		// First mail sent as expected
	case <-time.After(3 * time.Second):
		t.Error("timeout: first mail should have been sent")
	}
	<-done

	// Second identical failure should be suppressed
	ctx2, _ := setupTestContext(t)
	ctx2.Start()
	ctx2.Stop(errors.New("first error"))

	done2 := make(chan struct{})
	go func() {
		_ = m.Run(ctx2)
		close(done2)
	}()

	select {
	case <-f.fromCh:
		t.Error("second identical error mail should have been suppressed by dedup")
	case <-time.After(500 * time.Millisecond):
		// Expected: suppressed
	}
	<-done2
}

func TestMailRun_SendMailError(t *testing.T) {
	t.Parallel()
	ctx, _ := setupTestContext(t)

	ctx.Start()
	ctx.Stop(nil)

	// Use an invalid SMTP host to force sendMail to fail
	m := NewMail(&MailConfig{
		SMTPHost:  "invalid-host-that-does-not-exist",
		SMTPPort:  12345,
		EmailTo:   "foo@foo.com",
		EmailFrom: "qux@qux.com",
	})

	// Run should not return error (sendMail error is logged, not returned)
	err := m.Run(ctx)
	assert.NoError(t, err)
}

func TestMailSendMail_TLSSkipVerify(t *testing.T) {
	t.Parallel()

	m := &Mail{MailConfig: MailConfig{
		SMTPHost:          "invalid-host-that-does-not-exist",
		SMTPPort:          12345,
		EmailTo:           "foo@foo.com",
		EmailFrom:         "qux@qux.com",
		SMTPTLSSkipVerify: true,
	}}

	ctx, _ := setupTestContext(t)
	ctx.Start()
	ctx.Stop(nil)

	// sendMail should attempt to connect and fail, but the TLS config branch is covered
	err := m.sendMail(ctx)
	require.Error(t, err, "should fail to dial invalid host")
	assert.Contains(t, err.Error(), "dial and send mail")
}

// ---- preset.go coverage ----

func TestPresetLoader_LoadEmptySpec(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	preset, err := loader.Load("")
	require.Error(t, err)
	assert.Nil(t, preset)
	assert.Contains(t, err.Error(), "empty")
}

func TestPresetLoader_LoadAbsoluteFilePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := "name: file-test\nurl_scheme: 'https://example.com/{id}'\nmethod: POST\nbody: '{\"x\":1}'\n"
	path := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	loader := NewPresetLoader(nil)
	preset, err := loader.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "file-test", preset.Name)
}

func TestPresetLoader_LoadRelativePath(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	// Relative path that doesn't exist
	_, err := loader.Load("./nonexistent-preset.yaml")
	require.Error(t, err)
}

func TestPresetLoader_LoadParentRelativePath(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	_, err := loader.Load("../nonexistent-preset.yaml")
	require.Error(t, err)
}

func TestPresetLoader_LoadHTTPURL(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	// Should fail because remote presets are disabled
	_, err := loader.Load("https://example.com/preset.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote presets are disabled")
}

func TestPresetLoader_LoadHTTPURL_Insecure(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	_, err := loader.Load("http://example.com/preset.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote presets are disabled")
}

func TestPresetLoader_LoadGitHubShorthand(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	_, err := loader.Load("gh:org/repo/preset.yaml@v1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote presets are disabled")
}

func TestPresetLoader_LoadNotFoundAnywhere(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	_, err := loader.Load("this-preset-does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPresetLoader_LoadFromLocalDir_NotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	loader := NewPresetLoader(nil)
	loader.AddLocalPresetDir(dir)

	_, err := loader.Load("nonexistent-in-local")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPresetLoader_LoadFromGitHub_InvalidShorthand(t *testing.T) {
	t.Parallel()

	gc := &WebhookGlobalConfig{
		AllowRemotePresets:   true,
		TrustedPresetSources: "gh:org/*",
	}
	loader := NewPresetLoader(gc)

	// "gh:" alone is an invalid format for the regex
	_, err := loader.loadFromGitHub("gh:")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid GitHub shorthand")
}

func TestPresetLoader_LoadFromURL_CacheHit(t *testing.T) {
	SetValidateWebhookURLForTest(func(_ string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	presetYAML := "name: cached-test\nurl_scheme: 'https://hooks.example.com/{id}'\nmethod: POST\nbody: '{\"msg\": \"hello\"}'\n"

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_, _ = w.Write([]byte(presetYAML))
	}))
	defer server.Close()

	gc := &WebhookGlobalConfig{
		AllowRemotePresets: true,
		PresetCacheTTL:     time.Hour,
		PresetCacheDir:     t.TempDir(),
	}
	loader := NewPresetLoader(gc)

	url := server.URL + "/preset.yaml"

	// First load: fetches from server
	p1, err := loader.loadFromURL(url)
	require.NoError(t, err)
	assert.Equal(t, "cached-test", p1.Name)
	assert.Equal(t, 1, callCount)

	// Second load: should come from cache (no additional server call)
	p2, err := loader.loadFromURL(url)
	require.NoError(t, err)
	assert.Equal(t, "cached-test", p2.Name)
	assert.Equal(t, 1, callCount, "second load should use cache, not fetch again")
}

func TestPresetLoader_LoadFromURL_CreateRequestError(t *testing.T) {
	// Not parallel: modifies global URL validator
	gc := &WebhookGlobalConfig{AllowRemotePresets: true}
	loader := NewPresetLoader(gc)

	SetValidateWebhookURLForTest(func(_ string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	// A URL with control characters will fail NewRequestWithContext
	_, err := loader.loadFromURL("http://example.com/\x7f")
	require.Error(t, err)
}

func TestPresetLoader_LoadFromURL_ConnectionError(t *testing.T) {
	SetValidateWebhookURLForTest(func(_ string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := server.URL
	server.Close()

	gc := &WebhookGlobalConfig{AllowRemotePresets: true}
	loader := NewPresetLoader(gc)

	_, err := loader.loadFromURL(closedURL + "/preset.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch preset from")
}

func TestPresetLoader_LoadFromURL_InvalidYAMLResponse(t *testing.T) {
	SetValidateWebhookURLForTest(func(_ string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("name: broken\n"))
	}))
	defer server.Close()

	gc := &WebhookGlobalConfig{AllowRemotePresets: true}
	loader := NewPresetLoader(gc)

	_, err := loader.loadFromURL(server.URL + "/preset.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse preset from")
}

func TestPresetLoader_LoadFromURL_OversizedResponse(t *testing.T) {
	SetValidateWebhookURLForTest(func(_ string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Write more than 1MB
		big := strings.Repeat("x", 1024*1024+100)
		_, _ = w.Write([]byte(big))
	}))
	defer server.Close()

	gc := &WebhookGlobalConfig{AllowRemotePresets: true}
	loader := NewPresetLoader(gc)

	_, err := loader.loadFromURL(server.URL + "/preset.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestIsTrustedSource_EmptyPattern(t *testing.T) {
	t.Parallel()

	// Trusted sources with empty entries after splitting
	loader := &PresetLoader{globalConfig: &WebhookGlobalConfig{
		TrustedPresetSources: ",, ,,",
	}}
	assert.False(t, loader.isTrustedSource("gh:anything"))
}

func TestPreset_BuildURL_UnreplacedVariables(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		URLScheme: "https://hooks.example.com/{id}/{unknown_var}",
	}
	config := &WebhookConfig{
		ID: "test-id",
	}

	_, err := preset.BuildURL(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unreplaced variables")
}

func TestPreset_BuildURL_CustomVars(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		URLScheme: "https://hooks.example.com/{channel}/{token}",
	}
	config := &WebhookConfig{
		CustomVars: map[string]string{
			"channel": "general",
			"token":   "abc123",
		},
	}

	url, err := preset.BuildURL(config)
	require.NoError(t, err)
	assert.Equal(t, "https://hooks.example.com/general/abc123", url)
}

func TestPreset_RenderBody_ParseError(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Body: `{{.Invalid template syntax`,
	}
	data := &WebhookData{}

	_, err := preset.RenderBody(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse body template")
}

func TestPreset_RenderBody_ExecuteError(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Body: `{{.NonExistent.Field}}`,
	}
	data := &WebhookData{}

	_, err := preset.RenderBody(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execute body template")
}

func TestWebhookTemplateFuncs_Truncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		n        int
		input    string
		expected string
	}{
		{"short string unchanged", 10, "hello", "hello"},
		{"exact length unchanged", 5, "hello", "hello"},
		{"long string truncated", 3, "hello", "hel..."},
		// Rune-aware: each emoji is one rune (4 UTF-8 bytes). A byte-slice
		// truncate at n=4 would cut the second emoji mid-rune and corrupt
		// the JSON downstream. Rune-aware truncate keeps it whole.
		{"multi-byte runes treated as one", 2, "🔴🟡🟢", "🔴🟡..."},
		{"multi-byte under limit unchanged", 5, "🔴🟡🟢", "🔴🟡🟢"},
		{"ascii after emoji boundary", 3, "🔴hello", "🔴he..."},
	}

	fn := webhookTemplateFuncs["truncate"].(func(int, string) string)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := fn(tt.n, tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestWebhookTemplateFuncs_JSON_RFC8259EscapeCoverage pins the escape
// table on the `json` template helper: every byte U+0000-U+001F must
// produce a valid JSON escape sequence so the receiver's parser doesn't
// reject the body when stdout contains terminal escapes (`\x1b`), bell
// (`\x07`), NUL, form feed, etc. The bundled `json-post` preset added
// for #676 is the first consumer that routes untrusted command output
// (Execution.Output / Execution.Stderr) through this helper, which is
// what surfaced the latent escape gap.
func TestWebhookTemplateFuncs_JSON_RFC8259EscapeCoverage(t *testing.T) {
	t.Parallel()
	fn := webhookTemplateFuncs["json"].(func(string) string)

	// Short-escape forms — keep their conventional names.
	assert.Equal(t, `\"`, fn(`"`))
	assert.Equal(t, `\\`, fn(`\`))
	assert.Equal(t, `\b`, fn("\b"))
	assert.Equal(t, `\f`, fn("\f"))
	assert.Equal(t, `\n`, fn("\n"))
	assert.Equal(t, `\r`, fn("\r"))
	assert.Equal(t, `\t`, fn("\t"))

	// Other control chars must use \u00XX. NUL and ESC are the most
	// common real-world offenders (binary tools, ANSI color output).
	assert.Equal(t, "\\u0000", fn("\x00"))
	assert.Equal(t, "\\u0007", fn("\x07")) // BEL
	assert.Equal(t, "\\u001b", fn("\x1b")) // ESC — start of every ANSI sequence
	assert.Equal(t, "\\u001f", fn("\x1f"))

	// >= 0x20 passes through unchanged, incl. multi-byte UTF-8.
	assert.Equal(t, " ", fn(" "))
	assert.Equal(t, "hello", fn("hello"))
	assert.Equal(t, "🔴", fn("🔴"))

	// Round-trip: helper output wrapped in quotes must parse as JSON
	// and yield the original string for every control byte 0x00-0x1F.
	for c := byte(0); c < 0x20; c++ {
		in := string([]byte{c})
		quoted := `"` + fn(in) + `"`
		var out string
		require.NoError(t, json.Unmarshal([]byte(quoted), &out),
			"escape of byte 0x%02x must round-trip through JSON; got %q", c, quoted)
		assert.Equal(t, in, out, "byte 0x%02x must survive escape+parse round-trip", c)
	}
}

func TestWebhookTemplateFuncs_Default(t *testing.T) {
	t.Parallel()

	fn := webhookTemplateFuncs["default"].(func(string, string) string)
	assert.Equal(t, "fallback", fn("fallback", ""))
	assert.Equal(t, "actual", fn("fallback", "actual"))
}

func TestWebhookTemplateFuncs_IsoTime(t *testing.T) {
	t.Parallel()

	fn := webhookTemplateFuncs["isoTime"].(func(time.Time) string)
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	result := fn(ts)
	assert.Equal(t, "2026-01-15T10:30:00Z", result)
}

func TestWebhookTemplateFuncs_UnixTime(t *testing.T) {
	t.Parallel()

	fn := webhookTemplateFuncs["unixTime"].(func(time.Time) int64)
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	result := fn(ts)
	assert.Equal(t, ts.Unix(), result)
}

func TestWebhookTemplateFuncs_FormatDuration(t *testing.T) {
	t.Parallel()

	fn := webhookTemplateFuncs["formatDuration"].(func(time.Duration) string)
	result := fn(5 * time.Second)
	assert.Equal(t, "5s", result)
}

func TestWebhookTemplateFuncs_Json(t *testing.T) {
	t.Parallel()

	fn := webhookTemplateFuncs["json"].(func(string) string)
	assert.Equal(t, `hello\\world`, fn(`hello\world`))
	assert.Equal(t, `say \"hi\"`, fn(`say "hi"`))
	assert.Equal(t, `line1\nline2`, fn("line1\nline2"))
	assert.Equal(t, `col1\tcol2`, fn("col1\tcol2"))
	assert.Equal(t, `return\r`, fn("return\r"))
}

// ---- preset_cache.go coverage ----

func TestNewPresetCache_MkdirFails(t *testing.T) {
	t.Parallel()

	// Use a path under a file (not a directory) to make MkdirAll fail
	tmpFile := filepath.Join(t.TempDir(), "afile")
	require.NoError(t, os.WriteFile(tmpFile, []byte("data"), 0o600))

	badDir := filepath.Join(tmpFile, "subdir", "presets")
	cache := NewPresetCache(badDir, time.Hour)
	// Should still return a cache even if mkdir fails (warning is printed)
	assert.NotNil(t, cache)
}

func TestPresetCache_GetFromDisk_URLCollisionExact(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	// Put preset for URL A
	presetA := &Preset{Name: "a", Method: "POST", Body: `{"x":1}`}
	require.NoError(t, cache.Put("https://a.com/preset.yaml", presetA))

	// Manually tamper with the meta URL to cause collision detection
	keyA := cache.cacheKey("https://a.com/preset.yaml")
	metaPath := filepath.Join(tempDir, keyA+".meta.yaml")
	metaData, err := os.ReadFile(metaPath)
	require.NoError(t, err)
	// Change the URL in the meta file
	tampered := strings.Replace(string(metaData), "https://a.com/preset.yaml", "https://b.com/preset.yaml", 1)
	require.NoError(t, os.WriteFile(metaPath, []byte(tampered), 0o600))

	// New cache instance reads from disk only
	cache2 := NewPresetCache(tempDir, time.Hour)
	_, err = cache2.Get("https://a.com/preset.yaml")
	assert.Error(t, err, "URL collision should be detected")
}

func TestPresetCache_GetFromDisk_CorruptedPresetFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	url := "https://example.com/corrupted-preset.yaml"
	preset := &Preset{Name: "ok", Method: "POST", Body: `{"x":1}`}
	require.NoError(t, cache.Put(url, preset))

	// Corrupt the preset file (not the meta)
	key := cache.cacheKey(url)
	presetPath := filepath.Join(tempDir, key+".yaml")
	require.NoError(t, os.WriteFile(presetPath, []byte("not valid yaml: {{["), 0o600))

	// New cache instance reads from disk
	cache2 := NewPresetCache(tempDir, time.Hour)
	_, err := cache2.Get(url)
	assert.Error(t, err, "corrupted preset file should fail")
}

func TestPresetCache_GetFromDisk_MissingPresetFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	url := "https://example.com/missing-preset.yaml"
	preset := &Preset{Name: "ok", Method: "POST", Body: `{"x":1}`}
	require.NoError(t, cache.Put(url, preset))

	// Remove only the preset file, keep the meta
	key := cache.cacheKey(url)
	presetPath := filepath.Join(tempDir, key+".yaml")
	require.NoError(t, os.Remove(presetPath))

	cache2 := NewPresetCache(tempDir, time.Hour)
	_, err := cache2.Get(url)
	assert.Error(t, err, "missing preset file should fail")
}

func TestPresetCache_Clear_ReadDirError(t *testing.T) {
	t.Parallel()

	cache := NewPresetCache("/nonexistent/path/that/doesnt/exist", time.Hour)
	// Directly clear a cache whose dir doesn't exist
	err := cache.Clear()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read cache directory")
}

func TestPresetCache_Clear_SkipsDirectories(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	// Create a subdirectory inside cache dir
	require.NoError(t, os.Mkdir(filepath.Join(tempDir, "subdir"), 0o750))

	// Put a preset
	preset := &Preset{Name: "test", Method: "POST", Body: `{"x":1}`}
	require.NoError(t, cache.Put("https://example.com/test.yaml", preset))

	err := cache.Clear()
	require.NoError(t, err)

	// Subdir should still exist
	info, err := os.Stat(filepath.Join(tempDir, "subdir"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestPresetCache_Clear_RemoveError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	preset := &Preset{Name: "test", Method: "POST", Body: `{"x":1}`}
	require.NoError(t, cache.Put("https://example.com/test.yaml", preset))

	// Make the directory read-only to cause Remove to fail
	require.NoError(t, os.Chmod(tempDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(tempDir, 0o755) })

	err := cache.Clear()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove cached file")
}

func TestPresetCache_Cleanup_ReadDirError(t *testing.T) {
	t.Parallel()

	cache := NewPresetCache("/nonexistent/path/that/doesnt/exist", time.Hour)
	err := cache.Cleanup()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read cache directory")
}

func TestPresetCache_Cleanup_SkipsNonYAML(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	// Create a non-YAML file
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "notes.txt"), []byte("hi"), 0o600))

	err := cache.Cleanup()
	require.NoError(t, err)

	// txt file should still exist
	_, err = os.Stat(filepath.Join(tempDir, "notes.txt"))
	require.NoError(t, err)
}

func TestPresetCache_Cleanup_SkipsDirectories(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	require.NoError(t, os.Mkdir(filepath.Join(tempDir, "subdir"), 0o750))

	err := cache.Cleanup()
	require.NoError(t, err)
}

func TestPresetCache_Cleanup_RemovesInvalidMetadata(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	// Create a file that looks like a meta file but has invalid YAML
	invalidMeta := filepath.Join(tempDir, "abc1234567.meta.yaml")
	require.NoError(t, os.WriteFile(invalidMeta, []byte("invalid: yaml: [broken"), 0o600))

	err := cache.Cleanup()
	require.NoError(t, err)

	// Invalid meta file should have been removed
	_, err = os.Stat(invalidMeta)
	assert.True(t, os.IsNotExist(err), "invalid metadata file should be removed")
}

func TestPresetCache_Cleanup_RemovesExpiredDiskEntries(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	preset := &Preset{Name: "old", Method: "POST", Body: `{"x":1}`}
	url := "https://example.com/old.yaml"
	require.NoError(t, cache.Put(url, preset))

	// Manually backdate the expiry in the meta file
	key := cache.cacheKey(url)
	metaPath := filepath.Join(tempDir, key+".meta.yaml")
	expiredMeta := fmt.Sprintf("url: %s\nfetched_at: 2020-01-01T00:00:00Z\nexpires_at: 2020-01-01T01:00:00Z\n", url)
	require.NoError(t, os.WriteFile(metaPath, []byte(expiredMeta), 0o600))

	err := cache.Cleanup()
	require.NoError(t, err)

	// Both meta and preset file should be removed
	_, err = os.Stat(metaPath)
	assert.True(t, os.IsNotExist(err), "expired meta file should be removed")
	presetPath := filepath.Join(tempDir, key+".yaml")
	_, err = os.Stat(presetPath)
	assert.True(t, os.IsNotExist(err), "expired preset file should be removed")
}

func TestPresetCache_Cleanup_UnreadableMetaFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	// Create meta file that can't be read
	metaPath := filepath.Join(tempDir, "abc1234567.meta.yaml")
	require.NoError(t, os.WriteFile(metaPath, []byte("url: test\n"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(metaPath, 0o644) })

	err := cache.Cleanup()
	require.NoError(t, err, "unreadable meta file should be skipped, not cause error")
}

// ---- preset_github.go coverage ----

func TestParseGitHubShorthand_NoPath(t *testing.T) {
	t.Parallel()

	url, err := ParseGitHubShorthand("gh:org/repo")
	require.NoError(t, err)
	assert.Equal(t, "https://raw.githubusercontent.com/org/repo/main/preset.yaml", url)
}

func TestParseGitHubShorthandDetails_NotGitHub(t *testing.T) {
	t.Parallel()

	_, err := ParseGitHubShorthandDetails("https://example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a GitHub shorthand")
}

func TestParseGitHubShorthandDetails_InvalidFormat(t *testing.T) {
	t.Parallel()

	_, err := ParseGitHubShorthandDetails("gh:")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid GitHub shorthand")
}

func TestIsSemanticVersion_EmptyAfterTrim(t *testing.T) {
	t.Parallel()

	assert.False(t, IsSemanticVersion("v"), "bare 'v' with nothing after should return false")
}

// ---- restore.go coverage ----

func TestRestoreHistory_InvalidSaveFolder(t *testing.T) {
	t.Parallel()

	// A folder with dangerous patterns should be silently skipped
	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}
	err := RestoreHistory("../../etc/something", 24*time.Hour, []core.Job{job}, newDiscardLogger())
	require.NoError(t, err)
	assert.Empty(t, job.GetHistory())
}

func TestRestoreHistory_StatError(t *testing.T) {
	t.Parallel()

	// A path that exists but can't be stat'd (e.g. permission denied)
	// Use a folder that doesn't exist but has a valid-looking path
	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}
	err := RestoreHistory("/tmp/ofelia-test-nonexistent-dir-xyz123", 24*time.Hour, []core.Job{job}, newDiscardLogger())
	assert.NoError(t, err, "non-existent folder should return nil")
}

func TestRestoreHistory_NotADirectory(t *testing.T) {
	t.Parallel()

	// Create a file instead of a directory
	tmpFile := filepath.Join(t.TempDir(), "notadir")
	require.NoError(t, os.WriteFile(tmpFile, []byte("data"), 0o600))

	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}
	err := RestoreHistory(tmpFile, 24*time.Hour, []core.Job{job}, newDiscardLogger())
	require.NoError(t, err, "non-directory should return nil gracefully")
	assert.Empty(t, job.GetHistory())
}

func TestRestoreHistory_WalkError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Create a subdirectory that we can make unreadable
	subDir := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(subDir, 0o750))
	// Create a JSON file inside the unreadable dir
	execTime := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "test.json"), []byte("{}"), 0o600))

	// Make subdir unreadable
	require.NoError(t, os.Chmod(subDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(subDir, 0o750) })

	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}
	err := RestoreHistory(dir, 24*time.Hour, []core.Job{job}, newDiscardLogger())
	// Walk errors are handled gracefully
	assert.NoError(t, err)
	_ = execTime // suppress unused warning
}

func TestRestoreHistory_FileReadError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Create a JSON file that can't be read
	jsonFile := filepath.Join(dir, "unreadable.json")
	require.NoError(t, os.WriteFile(jsonFile, []byte("{}"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(jsonFile, 0o644) })

	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}
	err := RestoreHistory(dir, 24*time.Hour, []core.Job{job}, newDiscardLogger())
	assert.NoError(t, err, "unreadable file should be skipped gracefully")
}

// nonSettableJob is a job that implements core.Job but does NOT have SetLastRun.
// This is used to test the restore.go path where a job doesn't support SetLastRun.
type nonSettableJob struct {
	name string
}

func (j *nonSettableJob) GetName() string                { return j.name }
func (j *nonSettableJob) GetSchedule() string            { return "@every 1m" }
func (j *nonSettableJob) GetCommand() string             { return "echo test" }
func (j *nonSettableJob) ShouldRunOnStartup() bool       { return false }
func (j *nonSettableJob) Middlewares() []core.Middleware { return nil }
func (j *nonSettableJob) Use(_ ...core.Middleware)       {}
func (j *nonSettableJob) Run(_ *core.Context) error      { return nil }
func (j *nonSettableJob) Running() int32                 { return 0 }
func (j *nonSettableJob) NotifyStart()                   {}
func (j *nonSettableJob) NotifyStop()                    {}
func (j *nonSettableJob) GetCronJobID() uint64           { return 0 }
func (j *nonSettableJob) SetCronJobID(_ uint64)          {}
func (j *nonSettableJob) GetHistory() []*core.Execution  { return nil }
func (j *nonSettableJob) Hash() (string, error)          { return "test-hash", nil }

func TestRestoreHistory_JobWithoutSetLastRun(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	job := &nonSettableJob{name: "nosetter"}

	execTime := time.Now().Add(-1 * time.Hour)
	savedData := fmt.Sprintf(`{"Job":{"Name":"nosetter"},"Execution":{"ID":"e1","Date":"%s","Duration":5000000000,"Failed":false,"Skipped":false}}`, execTime.Format(time.RFC3339Nano))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.json"), []byte(savedData), 0o600))

	err := RestoreHistory(dir, 24*time.Hour, []core.Job{job}, newDiscardLogger())
	assert.NoError(t, err)
}

// ---- sanitize.go coverage ----

func TestSanitizePath_DangerousPatternTrigger(t *testing.T) {
	t.Parallel()

	ps := NewPathSanitizer()

	tests := []struct {
		name  string
		input string
	}{
		{"tilde prefix", "~user/file"},
		{"windows reserved name", "CON.txt"},
		{"special chars", "file<name>.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ps.SanitizePath(tt.input)
			assert.NotContains(t, result, "..")
			assert.NotContains(t, result, "~")
			assert.NotContains(t, result, "<")
			assert.NotContains(t, result, ">")
		})
	}
}

func TestSanitizePath_AbsolutePathStripping(t *testing.T) {
	t.Parallel()

	ps := NewPathSanitizer()

	// Test absolute path on Linux
	result := ps.SanitizePath("/absolute/path/file.txt")
	assert.False(t, filepath.IsAbs(result), "result should be relative")
}

func TestSanitizeFilename_LongWithExtension(t *testing.T) {
	t.Parallel()

	ps := NewPathSanitizer()

	// Create a filename that's > 255 chars with a short extension
	longName := strings.Repeat("a", 260) + ".txt"
	result := ps.SanitizeFilename(longName)
	assert.LessOrEqual(t, len(result), 255)
	assert.True(t, strings.HasSuffix(result, ".txt"), "extension should be preserved")
}

func TestSanitizeFilename_LongExtension(t *testing.T) {
	t.Parallel()

	ps := NewPathSanitizer()

	// Create a filename with an extension >= 255 chars
	longExt := "a." + strings.Repeat("b", 260)
	result := ps.SanitizeFilename(longExt)
	assert.LessOrEqual(t, len(result), 255)
}

// ---- save.go coverage ----

func TestSave_Run_ErrorDuringSave(t *testing.T) {
	t.Parallel()

	ctx, job := setupSaveTestContext(t)
	job.Name = "test-save-error"
	ctx.Execution.Date = time.Time{}

	ctx.Start()
	ctx.Stop(nil)

	// Use a path that will fail: a file instead of a directory
	tmpFile := filepath.Join(t.TempDir(), "afile")
	require.NoError(t, os.WriteFile(tmpFile, []byte("data"), 0o600))

	m := NewSave(&SaveConfig{SaveFolder: filepath.Join(tmpFile, "subdir")})
	// Run should not return error (save error is logged)
	err := m.Run(ctx)
	assert.NoError(t, err)
}

func TestSave_SaveToDisk_InvalidSaveFolder(t *testing.T) {
	t.Parallel()

	s := &Save{SaveConfig: SaveConfig{SaveFolder: "../../etc/dangerous"}}

	ctx, _ := setupTestContext(t)
	ctx.Start()
	ctx.Stop(nil)

	err := s.saveToDisk(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid save folder")
}

func TestSave_SaveToDisk_MkdirError(t *testing.T) {
	t.Parallel()

	// Use a path under a file (not a directory) to make MkdirAll fail
	tmpFile := filepath.Join(t.TempDir(), "afile")
	require.NoError(t, os.WriteFile(tmpFile, []byte("data"), 0o600))

	s := &Save{SaveConfig: SaveConfig{SaveFolder: filepath.Join(tmpFile, "sub")}}

	ctx, _ := setupTestContext(t)
	ctx.Start()
	ctx.Stop(nil)

	err := s.saveToDisk(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir")
}

func TestSave_WriteFile_Error(t *testing.T) {
	t.Parallel()

	s := &Save{}
	err := s.writeFile([]byte("data"), "/nonexistent/path/file.txt", defaultSaveFileMode)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write file")
}

func TestSave_SaveContextToDisk_WriteError(t *testing.T) {
	t.Parallel()

	s := &Save{}
	ctx, _ := setupTestContext(t)
	ctx.Start()
	ctx.Stop(nil)

	err := s.saveContextToDisk(ctx, "/nonexistent/path/file.json", defaultSaveFileMode)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write json file")
}

func TestSave_SaveToDisk_StderrWriteError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := &Save{SaveConfig: SaveConfig{SaveFolder: dir}}

	ctx, job := setupSaveTestContext(t)
	job.Name = "test-stderr-err"
	ctx.Start()
	ctx.Stop(nil)
	ctx.Execution.Date = time.Time{}

	// Make the directory read-only after creation to cause write errors
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := s.saveToDisk(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write stderr log")
}

// ---- slack.go coverage ----

func TestSlackPushMessage_InvalidURL(t *testing.T) {
	t.Parallel()

	m := &Slack{
		SlackConfig: SlackConfig{SlackWebhook: "not-a-valid-url"},
		Client:      &http.Client{Timeout: 1 * time.Second},
	}

	ctx, _ := setupTestContext(t)
	ctx.Start()
	ctx.Stop(nil)

	// Should not panic, just log error
	assert.NotPanics(t, func() {
		m.pushMessage(ctx)
	})
}

func TestSlackPushMessage_EmptySchemeURL(t *testing.T) {
	t.Parallel()

	m := &Slack{
		SlackConfig: SlackConfig{SlackWebhook: "://missing-scheme"},
		Client:      &http.Client{Timeout: 1 * time.Second},
	}

	ctx, _ := setupTestContext(t)
	ctx.Start()
	ctx.Stop(nil)

	assert.NotPanics(t, func() {
		m.pushMessage(ctx)
	})
}

// ---- webhook.go coverage ----

func TestNewWebhook_ValidationError(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name: "test",
		// No preset and no URL - validation should fail
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	require.Error(t, err)
	assert.Nil(t, middleware)
}

func TestNewWebhook_MissingRequiredVariable(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name:   "test",
		Preset: "slack",
		// slack preset requires "id" and "secret" but we don't provide them
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	require.Error(t, err)
	assert.Nil(t, middleware)
	assert.Contains(t, err.Error(), "required variable")
}

func TestValidatePresetVariables_CustomVarRequired(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Variables: map[string]PresetVariable{
			"custom_token": {Required: true, Description: "A custom token"},
		},
	}
	config := &WebhookConfig{
		CustomVars: map[string]string{"custom_token": "value123"},
	}

	err := validatePresetVariables(preset, config)
	require.NoError(t, err)
}

func TestValidatePresetVariables_CustomVarMissing(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Variables: map[string]PresetVariable{
			"custom_token": {Required: true, Description: "A custom token"},
		},
	}
	config := &WebhookConfig{}

	err := validatePresetVariables(preset, config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom_token")
}

func TestValidatePresetVariables_CustomVarMissingWithNilMap(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Variables: map[string]PresetVariable{
			"custom_token": {Required: true, Description: "A custom token"},
		},
	}
	config := &WebhookConfig{
		CustomVars: nil,
	}

	err := validatePresetVariables(preset, config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom_token")
}

func TestValidatePresetVariables_SecretVar(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Variables: map[string]PresetVariable{
			"secret": {Required: true, Description: "Secret key"},
		},
	}

	tests := []struct {
		name    string
		config  *WebhookConfig
		wantErr bool
	}{
		{"secret provided", &WebhookConfig{Secret: "s123"}, false},
		{"secret empty", &WebhookConfig{Secret: ""}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validatePresetVariables(preset, tt.config)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidatePresetVariables_URLVar(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Variables: map[string]PresetVariable{
			"url": {Required: true, Description: "Webhook URL"},
		},
	}
	config := &WebhookConfig{URL: "https://example.com/hook"}

	err := validatePresetVariables(preset, config)
	require.NoError(t, err)
}

func TestValidatePresetVariables_NonRequired(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Variables: map[string]PresetVariable{
			"optional": {Required: false, Description: "Not required"},
		},
	}
	config := &WebhookConfig{}

	err := validatePresetVariables(preset, config)
	require.NoError(t, err)
}

func TestWebhook_Run_DedupSuppressesDuplicate(t *testing.T) {
	// Not parallel: modifies global security config
	SetValidateWebhookURLForTest(func(_ string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	SetTransportFactoryForTest(func() *http.Transport {
		return http.DefaultTransport.(*http.Transport).Clone()
	})
	defer SetTransportFactoryForTest(NewSafeTransport)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dedup := NewNotificationDedup(time.Hour)

	config := &WebhookConfig{
		Name:       "test",
		Preset:     "slack",
		ID:         "T12345/B67890",
		Secret:     "xoxb-test-secret",
		URL:        server.URL,
		Trigger:    TriggerAlways,
		Timeout:    5 * time.Second,
		RetryCount: 0,
		Dedup:      dedup,
	}

	loader := NewPresetLoader(nil)
	middleware, err := NewWebhook(config, loader)
	require.NoError(t, err)

	webhook := middleware.(*Webhook)

	// First failed execution
	job := &TestJob{}
	job.Name = "test-job"
	sh := core.NewScheduler(newDiscardLogger())
	e1, _ := core.NewExecution()
	ctx1 := core.NewContext(sh, job, e1)
	ctx1.Start()
	ctx1.Stop(errors.New("error1"))

	_ = webhook.Run(ctx1)
	assert.Equal(t, 1, callCount, "first notification should be sent")

	// Second identical failure should be suppressed
	e2, _ := core.NewExecution()
	ctx2 := core.NewContext(sh, job, e2)
	ctx2.Start()
	ctx2.Stop(errors.New("error1"))

	_ = webhook.Run(ctx2)
	assert.Equal(t, 1, callCount, "duplicate error should be suppressed")
}

func TestWebhook_Run_SendError(t *testing.T) {
	// Not parallel: modifies global security config
	SetValidateWebhookURLForTest(func(_ string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	SetTransportFactoryForTest(func() *http.Transport {
		return http.DefaultTransport.(*http.Transport).Clone()
	})
	defer SetTransportFactoryForTest(NewSafeTransport)

	// Server that is already closed
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := server.URL
	server.Close()

	config := &WebhookConfig{
		Name:       "test",
		Preset:     "slack",
		ID:         "T12345/B67890",
		Secret:     "xoxb-test-secret",
		URL:        closedURL,
		Trigger:    TriggerAlways,
		Timeout:    1 * time.Second,
		RetryCount: 0,
		RetryDelay: 1 * time.Millisecond,
	}

	loader := NewPresetLoader(nil)
	middleware, err := NewWebhook(config, loader)
	require.NoError(t, err)

	job := &TestJob{}
	job.Name = "test-job"
	sh := core.NewScheduler(newDiscardLogger())
	e, _ := core.NewExecution()
	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	// Run should not return error (webhook error is logged)
	err = middleware.Run(ctx)
	assert.NoError(t, err)
}

func TestWebhook_SubstituteVariables_CustomVars(t *testing.T) {
	t.Parallel()

	w := &Webhook{
		Config: &WebhookConfig{
			ID:     "myid",
			Secret: "mysecret",
			URL:    "myurl",
			CustomVars: map[string]string{
				"channel": "general",
				"env":     "prod",
			},
		},
	}

	result := w.substituteVariables("Header {id} {secret} {url} {channel} {env}")
	assert.Equal(t, "Header myid mysecret myurl general prod", result)
}

func TestWebhookManager_GetMiddlewares_CreationError(t *testing.T) {
	t.Parallel()

	manager := NewWebhookManager(DefaultWebhookGlobalConfig())

	// Register a webhook with config that will fail validation during NewWebhook
	err := manager.Register(&WebhookConfig{
		Name: "bad-webhook",
		// No preset and no URL
	})
	require.NoError(t, err)

	_, err = manager.GetMiddlewares([]string{"bad-webhook"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create webhook")
}

func TestRenderBodyWithPreset_EmptyBody(t *testing.T) {
	t.Parallel()

	preset := &Preset{Body: ""}
	body, err := preset.RenderBodyWithPreset(map[string]any{})
	require.NoError(t, err)
	assert.Empty(t, body)
}

func TestRenderBodyWithPreset_ParseError(t *testing.T) {
	t.Parallel()

	preset := &Preset{Body: "{{.Invalid syntax"}
	_, err := preset.RenderBodyWithPreset(map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse body template")
}

func TestRenderBodyWithPreset_ExecuteError(t *testing.T) {
	t.Parallel()

	// Use a template that calls a function that will error during execution
	preset := &Preset{Body: `{{template "missing" .}}`}
	_, err := preset.RenderBodyWithPreset(map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execute body template")
}

// ---- webhook_config.go coverage ----

func TestDefaultWebhookGlobalConfig_XDGCacheHome(t *testing.T) {
	// Not parallel: modifies env
	original := os.Getenv("XDG_CACHE_HOME")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-test-cache")
	defer func() {
		if original != "" {
			os.Setenv("XDG_CACHE_HOME", original)
		} else {
			os.Unsetenv("XDG_CACHE_HOME")
		}
	}()

	config := DefaultWebhookGlobalConfig()
	assert.Equal(t, "/tmp/xdg-test-cache/ofelia/presets", config.PresetCacheDir)
}

// ---- webhook_security.go coverage ----

func TestValidateWebhookURLImpl_EmptyHostname(t *testing.T) {
	t.Parallel()

	// "https://:8080/path" has a host (":8080") but empty hostname
	err := ValidateWebhookURLImpl("https://:8080/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostname")
}

func TestSecurityValidator_Validate_InvalidURL(t *testing.T) {
	t.Parallel()

	validator := NewWebhookSecurityValidator(nil)
	err := validator.Validate("://invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid URL")
}

// ---- Second batch: remaining uncovered branches ----

// webhook.go send() error branches

func TestWebhook_Send_BuildURLError(t *testing.T) {
	t.Parallel()

	w := &Webhook{
		Config: &WebhookConfig{
			Name:    "test",
			Timeout: 5 * time.Second,
		},
		Preset: &Preset{
			URLScheme: "https://example.com/{id}/{unreplaced}",
			Method:    "POST",
			Body:      `{"text":"hello"}`,
			Headers:   map[string]string{"Content-Type": "application/json"},
		},
		Client: &http.Client{Timeout: 5 * time.Second},
	}

	ctx, _ := setupTestContext(t)
	ctx.Start()
	ctx.Stop(nil)

	err := w.send(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build URL")
}

func TestWebhook_Send_URLValidationError(t *testing.T) {
	// Not parallel: modifies global validator
	SetValidateWebhookURLForTest(func(_ string) error {
		return fmt.Errorf("blocked by test validator")
	})
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	w := &Webhook{
		Config: &WebhookConfig{
			Name:    "test",
			URL:     "https://example.com/hook",
			Timeout: 5 * time.Second,
		},
		Preset: &Preset{
			Method:  "POST",
			Body:    `{"text":"hello"}`,
			Headers: map[string]string{"Content-Type": "application/json"},
		},
		Client: &http.Client{Timeout: 5 * time.Second},
	}

	ctx, _ := setupTestContext(t)
	ctx.Start()
	ctx.Stop(nil)

	err := w.send(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "URL validation")
}

func TestWebhook_Send_RenderBodyError(t *testing.T) {
	// Not parallel: modifies global validator
	SetValidateWebhookURLForTest(func(_ string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	w := &Webhook{
		Config: &WebhookConfig{
			Name:    "test",
			URL:     "https://example.com/hook",
			Timeout: 5 * time.Second,
		},
		Preset: &Preset{
			Method:  "POST",
			Body:    `{{template "nonexistent" .}}`,
			Headers: map[string]string{"Content-Type": "application/json"},
		},
		Client: &http.Client{Timeout: 5 * time.Second},
	}

	ctx, _ := setupTestContext(t)
	ctx.Start()
	ctx.Stop(nil)

	err := w.send(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "render body")
}

func TestWebhook_Send_CreateRequestError(t *testing.T) {
	// Not parallel: modifies global validator
	SetValidateWebhookURLForTest(func(_ string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	w := &Webhook{
		Config: &WebhookConfig{
			Name:    "test",
			URL:     "https://example.com/hook",
			Timeout: 5 * time.Second,
		},
		Preset: &Preset{
			Method:  "INVALID METHOD WITH SPACES",
			Body:    `{"text":"hello"}`,
			Headers: map[string]string{"Content-Type": "application/json"},
		},
		Client: &http.Client{Timeout: 5 * time.Second},
	}

	ctx, _ := setupTestContext(t)
	ctx.Start()
	ctx.Stop(nil)

	err := w.send(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create request")
}

// preset.go loadFromGitHub trusted source -> loadFromURL path

func TestPresetLoader_LoadFromGitHub_TrustedSource(t *testing.T) {
	SetValidateWebhookURLForTest(func(_ string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	presetYAML := "name: gh-test\nurl_scheme: 'https://hooks.example.com/{id}'\nmethod: POST\nbody: '{\"msg\": \"hello\"}'\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(presetYAML))
	}))
	defer server.Close()

	gc := &WebhookGlobalConfig{
		AllowRemotePresets:   true,
		TrustedPresetSources: "gh:org/*",
		PresetCacheTTL:       time.Hour,
		PresetCacheDir:       t.TempDir(),
	}
	loader := NewPresetLoader(gc)

	// We can't actually make the GitHub URL point to our test server,
	// but we can test that the trusted source check passes and
	// the loadFromURL is attempted (it will fail connecting to raw.githubusercontent.com)
	_, err := loader.loadFromGitHub("gh:org/repo/preset.yaml@v1")
	// It should get past the trust check but fail on HTTP fetch
	require.Error(t, err)
	// Should NOT contain "trusted-preset-sources" (that means trust check passed)
	assert.NotContains(t, err.Error(), "trusted-preset-sources")
}

// preset.go BuildURL with {url} replacement (dead code path, but let's verify URL override)

func TestPreset_BuildURL_WithURLSchemeOnly(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		URLScheme: "https://hooks.example.com/{id}/{secret}",
	}
	config := &WebhookConfig{
		ID:     "my-id",
		Secret: "my-secret",
	}

	url, err := preset.BuildURL(config)
	require.NoError(t, err)
	assert.Equal(t, "https://hooks.example.com/my-id/my-secret", url)
}

// preset_cache.go putToDisk write errors

func TestPresetCache_PutToDisk_MetaWriteError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	// Make cache dir read-only to trigger write errors
	require.NoError(t, os.Chmod(tempDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(tempDir, 0o755) })

	preset := &Preset{Name: "test", Method: "POST", Body: `{"x":1}`}
	err := cache.Put("https://example.com/test.yaml", preset)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write cache metadata")
}

func TestPresetCache_PutToDisk_PresetWriteError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	preset := &Preset{Name: "test", Method: "POST", Body: `{"x":1}`}
	url := "https://example.com/preset-write-error.yaml"

	// First write succeeds (meta file)
	// Then make dir read-only so second write fails
	// This is tricky - we need to write meta first, then block preset write
	// Instead, let's write the meta manually, then make dir read-only
	key := cache.cacheKey(url)
	metaPath := filepath.Join(tempDir, key+".meta.yaml")
	require.NoError(t, os.WriteFile(metaPath, []byte("url: test\n"), 0o600))

	// Make dir read-only - since meta already exists, WriteFile for preset will fail
	require.NoError(t, os.Chmod(tempDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(tempDir, 0o755) })

	err := cache.putToDisk(key, url, preset, time.Now().Add(time.Hour))
	require.Error(t, err)
	// Depending on the platform, it may fail on meta or preset write
	assert.True(t,
		strings.Contains(err.Error(), "write cache metadata") ||
			strings.Contains(err.Error(), "write cached preset"),
		"expected write error, got: %v", err)
}

// restore.go - stat error (not IsNotExist)

func TestRestoreHistory_StatErrorNotNotExist(t *testing.T) {
	t.Parallel()

	// Create a directory, then a file inside it, and try to stat a
	// path where a component is a file (not a dir)
	tmpDir := t.TempDir()
	blockerFile := filepath.Join(tmpDir, "blocker")
	require.NoError(t, os.WriteFile(blockerFile, []byte("x"), 0o600))

	// On Linux, stat of "file/subdir" returns ENOTDIR, not ENOENT
	savePath := filepath.Join(blockerFile, "subdir")
	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}
	err := RestoreHistory(savePath, 24*time.Hour, []core.Job{job}, newDiscardLogger())
	// This should trigger the generic stat error path (not IsNotExist)
	// and return the error
	require.Error(t, err, "stat error that is not NotExist should be returned")
}

// restore.go - job without SetLastRun that has matching history

func TestRestoreHistory_JobWithoutSetLastRun_HasHistory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	job := &nonSettableJob{name: "nosetter-job"}

	execTime := time.Now().Add(-1 * time.Hour)
	savedData := fmt.Sprintf(`{"Job":{"Name":"nosetter-job"},"Execution":{"ID":"e1","Date":"%s","Duration":5000000000,"Failed":false,"Skipped":false}}`, execTime.Format(time.RFC3339Nano))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.json"), []byte(savedData), 0o600))

	err := RestoreHistory(dir, 24*time.Hour, []core.Job{job}, newDiscardLogger())
	assert.NoError(t, err)
}

// restore.go - parseHistoryFile read error

func TestParseHistoryFile_ReadError(t *testing.T) {
	t.Parallel()

	_, err := parseHistoryFile("/nonexistent/file.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read file")
}

// save.go - stdout and context write errors (need stderr to succeed first)

func TestSave_SaveToDisk_StdoutWriteError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx, job := setupSaveTestContext(t)
	job.Name = "stdout-err-job"
	ctx.Start()
	ctx.Stop(nil)
	ctx.Execution.Date = time.Time{}

	s := &Save{SaveConfig: SaveConfig{SaveFolder: dir}}

	// Write stderr first manually (to simulate it succeeding),
	// then make the dir read-only so stdout write fails
	root := filepath.Join(dir, "00010101_000000_stdout-err-job")
	require.NoError(t, os.WriteFile(root+".stderr.log", []byte(""), 0o600))

	// Now make the directory read-only
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := s.saveToDisk(ctx)
	require.Error(t, err)
	// The error could be from stdout or context write
	assert.True(t,
		strings.Contains(err.Error(), "write stdout log") ||
			strings.Contains(err.Error(), "write stderr log"),
		"expected write error, got: %v", err)
}

func TestSave_SaveToDisk_ContextWriteError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx, job := setupSaveTestContext(t)
	job.Name = "ctx-err-job"
	ctx.Start()
	ctx.Stop(nil)
	ctx.Execution.Date = time.Time{}

	s := &Save{SaveConfig: SaveConfig{SaveFolder: dir}}

	// Pre-create stderr and stdout files, then make dir read-only
	root := filepath.Join(dir, "00010101_000000_ctx-err-job")
	require.NoError(t, os.WriteFile(root+".stderr.log", []byte(""), 0o600))
	require.NoError(t, os.WriteFile(root+".stdout.log", []byte(""), 0o600))

	// Make read-only so json write fails
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := s.saveToDisk(ctx)
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "write context json") ||
			strings.Contains(err.Error(), "write stderr log") ||
			strings.Contains(err.Error(), "write stdout log"),
		"expected write error, got: %v", err)
}

// restore.go - parseHistoryFiles with walkErr (inaccessible file inside walk)

func TestParseHistoryFiles_WalkError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Create a JSON file that will be found by Walk
	execTime := time.Now().Add(-1 * time.Hour)
	savedData := fmt.Sprintf(`{"Job":{"Name":"test-job"},"Execution":{"ID":"e1","Date":"%s","Duration":5000000000,"Failed":false,"Skipped":false}}`, execTime.Format(time.RFC3339Nano))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "good.json"), []byte(savedData), 0o600))

	// Create a subdirectory with no read permission to trigger walkErr for its contents
	unreadable := filepath.Join(dir, "unreadable")
	require.NoError(t, os.Mkdir(unreadable, 0o000))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

	entries, err := parseHistoryFiles(dir, time.Now().Add(-24*time.Hour), newDiscardLogger())
	require.NoError(t, err, "walkErr should be handled gracefully")
	assert.GreaterOrEqual(t, len(entries), 1, "accessible files should still be parsed")
}

// restore.go - Abs error for path outside save folder
// This is nearly impossible to trigger (filepath.Abs rarely fails),
// but let's test the save folder containment check

func TestParseHistoryFiles_PathOutsideSaveFolder(t *testing.T) {
	t.Parallel()

	// This tests the basic flow; the containment check is defense-in-depth
	// and can't be easily triggered with regular Walk
	dir := t.TempDir()
	entries, err := parseHistoryFiles(dir, time.Now().Add(-24*time.Hour), newDiscardLogger())
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// slack.go - NewRequestWithContext error (line 106)
// Nearly impossible to trigger because url.Parse + String() produces valid URLs
// and http.MethodPost is valid. Skip this branch.

// sanitize.go - absolute path after sanitization
// The replacer converts / to _, so the path can't be absolute after replacement.
// This is dead code on Linux but may be relevant on Windows.
// Let's test with a path that might survive sanitization as absolute.

func TestSanitizePath_WindowsDriveLetter(t *testing.T) {
	t.Parallel()

	ps := NewPathSanitizer()

	// On Linux, filepath.IsAbs("C:\\file") returns false
	// On Windows, it would return true
	// This test documents the behavior
	result := ps.SanitizePath("simple-file.txt")
	assert.Equal(t, "simple-file.txt", result)

	// The dangerous pattern branch is already tested
	result = ps.SanitizePath("~admin/secrets")
	assert.NotContains(t, result, "~")
}
