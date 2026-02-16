package middlewares

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParsePreset_Validation_URLSchemeOrBody kills CONDITIONALS_NEGATION at preset.go:306
// The condition is: preset.URLScheme == "" && preset.Body == ""
// We must verify that:
//   - Both empty -> error
//   - URLScheme set, Body empty -> success
//   - URLScheme empty, Body set -> success
//   - Both set -> success
func TestParsePreset_Validation_URLSchemeOrBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		yaml      string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "both empty returns error",
			yaml:      "name: test\n",
			wantErr:   true,
			errSubstr: "must have either url_scheme or body",
		},
		{
			name:    "only url_scheme set is valid",
			yaml:    "name: test\nurl_scheme: https://example.com/{id}\n",
			wantErr: false,
		},
		{
			name:    "only body set is valid",
			yaml:    "name: test\nbody: '{\"msg\": \"hello\"}'\n",
			wantErr: false,
		},
		{
			name:    "both set is valid",
			yaml:    "name: test\nurl_scheme: https://example.com/{id}\nbody: '{\"msg\": \"hello\"}'\n",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			preset, err := ParsePreset([]byte(tt.yaml))
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				assert.Nil(t, preset)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, preset)
			}
		})
	}
}

// TestParsePreset_DefaultMethod kills CONDITIONALS_NEGATION at preset.go:311
// The condition is: if preset.Method == ""
// We must verify that:
//   - Empty method gets defaulted to POST
//   - Non-empty method is preserved
func TestParsePreset_DefaultMethod(t *testing.T) {
	t.Parallel()

	t.Run("empty method defaults to POST", func(t *testing.T) {
		t.Parallel()
		preset, err := ParsePreset([]byte("name: test\nurl_scheme: https://example.com\n"))
		require.NoError(t, err)
		assert.Equal(t, http.MethodPost, preset.Method)
	})

	t.Run("explicit GET method is preserved", func(t *testing.T) {
		t.Parallel()
		preset, err := ParsePreset([]byte("name: test\nurl_scheme: https://example.com\nmethod: GET\n"))
		require.NoError(t, err)
		assert.Equal(t, http.MethodGet, preset.Method)
	})

	t.Run("explicit PUT method is preserved", func(t *testing.T) {
		t.Parallel()
		preset, err := ParsePreset([]byte("name: test\nurl_scheme: https://example.com\nmethod: PUT\n"))
		require.NoError(t, err)
		assert.Equal(t, http.MethodPut, preset.Method)
	})
}

// TestParsePreset_DefaultContentType kills CONDITIONALS_NEGATION at preset.go:320
// The condition is: if preset.Method == http.MethodPost
// We must verify that:
//   - POST method gets default Content-Type header
//   - GET method does NOT get default Content-Type header
//   - POST with explicit Content-Type preserves it
func TestParsePreset_DefaultContentType(t *testing.T) {
	t.Parallel()

	t.Run("POST gets default application/json content type", func(t *testing.T) {
		t.Parallel()
		preset, err := ParsePreset([]byte("name: test\nurl_scheme: https://example.com\nmethod: POST\n"))
		require.NoError(t, err)
		assert.Equal(t, "application/json", preset.Headers["Content-Type"])
	})

	t.Run("GET does not get default content type", func(t *testing.T) {
		t.Parallel()
		preset, err := ParsePreset([]byte("name: test\nurl_scheme: https://example.com\nmethod: GET\n"))
		require.NoError(t, err)
		_, hasContentType := preset.Headers["Content-Type"]
		assert.False(t, hasContentType, "GET method should not have default Content-Type")
	})

	t.Run("POST with explicit content type is preserved", func(t *testing.T) {
		t.Parallel()
		yaml := "name: test\nurl_scheme: https://example.com\nmethod: POST\nheaders:\n  Content-Type: text/plain\n"
		preset, err := ParsePreset([]byte(yaml))
		require.NoError(t, err)
		assert.Equal(t, "text/plain", preset.Headers["Content-Type"])
	})

	t.Run("implicit POST (empty method) gets default content type", func(t *testing.T) {
		t.Parallel()
		preset, err := ParsePreset([]byte("name: test\nurl_scheme: https://example.com\n"))
		require.NoError(t, err)
		assert.Equal(t, http.MethodPost, preset.Method)
		assert.Equal(t, "application/json", preset.Headers["Content-Type"])
	})
}

// TestPreset_BuildURL_URLOverride kills CONDITIONALS_NEGATION at preset.go:346
// The condition at line 346 is the second check: if config.URL != "" in the body
// of BuildURL, after {url} substitution. But the real kill is at line 332:
// if config.URL != "" -> return config.URL (early return).
// Line 346 is: if config.URL != "" { url = strings.ReplaceAll(url, "{url}", config.URL) }
// Negating to config.URL == "" would mean the substitution is skipped when URL IS set.
func TestPreset_BuildURL_URLSubstitution(t *testing.T) {
	t.Parallel()

	t.Run("config URL set returns URL directly bypassing scheme", func(t *testing.T) {
		t.Parallel()
		preset := &Preset{
			Name:      "test",
			URLScheme: "https://default.example.com/{id}",
		}
		config := &WebhookConfig{
			URL: "https://custom.example.com/hook",
			ID:  "test-id",
		}
		url, err := preset.BuildURL(config)
		require.NoError(t, err)
		// When URL is set, it returns that URL directly (line 332-333)
		assert.Equal(t, "https://custom.example.com/hook", url)
	})

	t.Run("config URL empty uses scheme with substitutions", func(t *testing.T) {
		t.Parallel()
		preset := &Preset{
			Name:      "test",
			URLScheme: "https://hooks.example.com/{id}/{secret}",
		}
		config := &WebhookConfig{
			ID:     "my-id",
			Secret: "my-secret",
		}
		url, err := preset.BuildURL(config)
		require.NoError(t, err)
		assert.Equal(t, "https://hooks.example.com/my-id/my-secret", url)
	})

	t.Run("unreplaced variables cause error", func(t *testing.T) {
		t.Parallel()
		preset := &Preset{
			Name:      "test",
			URLScheme: "https://hooks.example.com/{unknown_var}",
		}
		config := &WebhookConfig{}
		_, err := preset.BuildURL(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unreplaced variables")
	})
}

// TestNewPresetLoader_LoadBundledPresetsSuccess kills CONDITIONALS_NEGATION at preset.go:69
// The condition is: if err := loader.loadBundledPresets(); err != nil
// Negating to err == nil would mean that the error branch runs on success,
// which would print a warning and potentially skip loading.
// We verify that bundled presets are correctly loaded (no error path taken).
func TestNewPresetLoader_LoadBundledPresetsSuccess(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)

	// Verify bundled presets are loaded (loadBundledPresets succeeded)
	presets := loader.ListBundledPresets()
	assert.NotEmpty(t, presets, "bundled presets should be loaded")

	// Verify at least one preset is usable
	preset, ok := loader.GetBundledPreset("slack")
	assert.True(t, ok, "slack preset should be available")
	assert.NotNil(t, preset)
	assert.Equal(t, "slack", preset.Name)
	assert.NotEmpty(t, preset.Body)
}
