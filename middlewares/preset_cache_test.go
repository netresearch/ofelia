// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPresetCache(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	assert.NotNil(t, cache)
	assert.Equal(t, time.Hour, cache.ttl)
	assert.Equal(t, tempDir, cache.cacheDir)
	assert.NotNil(t, cache.memory)
}

func TestNewPresetCache_DefaultDir(t *testing.T) {
	t.Parallel()

	cache := NewPresetCache("", time.Hour)

	assert.NotNil(t, cache)
	assert.NotEmpty(t, cache.cacheDir)
}

// TestNewPresetCache_DefaultDir_HardensExistingDir verifies that an
// upgrade from a prior loose-mode (0o755) cache directory is tightened
// to 0o700 on next ofelia start. os.MkdirAll alone does not adjust
// perms on pre-existing entries, so we explicitly chmod when we picked
// the path ourselves.
//
// We isolate the test by pointing XDG_CACHE_HOME at a t.TempDir; this
// uses the same code path NewPresetCache takes for the default location.
func TestNewPresetCache_DefaultDir_HardensExistingDir(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)

	preExisting := filepath.Join(xdg, "ofelia", "presets")
	require.NoError(t, os.MkdirAll(preExisting, 0o755))

	cache := NewPresetCache("", time.Hour)

	require.NotNil(t, cache)
	require.Equal(t, preExisting, cache.cacheDir)

	info, err := os.Stat(preExisting)
	require.NoError(t, err)
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("expected default cache dir to be tightened to 0o700, got %#o", perm)
	}
}

// TestNewPresetCache_DefaultDir_TempDirFallback verifies the fallback
// path used when os.UserCacheDir cannot determine a per-user location
// (no $HOME, no $XDG_CACHE_HOME, no platform default). The fallback
// must include the current UID so it cannot be pre-created as a symlink
// by another local user.
func TestNewPresetCache_DefaultDir_TempDirFallback(t *testing.T) {
	// Force os.UserCacheDir to fail by stripping every env var it consults.
	// On Linux it reads XDG_CACHE_HOME then HOME; on macOS HOME; on Windows
	// LocalAppData. Clearing all of them covers each platform.
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("LocalAppData", "")

	// Redirect TempDir so the test does not pollute the real /tmp.
	t.Setenv("TMPDIR", t.TempDir())

	got := defaultPresetCacheDir()

	uidNamespace := fmt.Sprintf("ofelia-%d", os.Getuid())
	if !strings.Contains(got, uidNamespace) {
		t.Errorf("expected TempDir fallback to include %q for UID isolation, got %q", uidNamespace, got)
	}
	if !strings.HasSuffix(got, filepath.Join(uidNamespace, "presets")) {
		t.Errorf("expected fallback path to end with %s/presets, got %q", uidNamespace, got)
	}
}

// TestNewPresetCache_ExplicitDir_PreservesPerms verifies that when a
// caller supplies their own cacheDir, NewPresetCache does NOT chmod it.
// Operators who configure a custom path are assumed to have set perms
// deliberately, and silently tightening them would be surprising.
func TestNewPresetCache_ExplicitDir_PreservesPerms(t *testing.T) {
	t.Parallel()

	explicit := filepath.Join(t.TempDir(), "explicit-cache")
	require.NoError(t, os.MkdirAll(explicit, 0o755))

	_ = NewPresetCache(explicit, time.Hour)

	info, err := os.Stat(explicit)
	require.NoError(t, err)
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Errorf("expected explicit cache dir perms to be preserved (0o755), got %#o", perm)
	}
}

func TestPresetCache_PutGet_Memory(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	preset := &Preset{
		Name:   "test-preset",
		Method: "POST",
		Body:   "test body",
	}

	testURL := "https://example.com/test.yaml"
	err := cache.Put(testURL, preset)
	require.NoError(t, err)

	retrieved, err := cache.Get(testURL)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, "test-preset", retrieved.Name)
}

func TestPresetCache_Get_NotFound(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	_, err := cache.Get("https://nonexistent.com/preset.yaml")
	assert.Error(t, err)
}

func TestPresetCache_Expiration(t *testing.T) {
	t.Parallel()

	// The TTL must outlast Put plus the first Get on a loaded runner: at
	// 10ms the first Get itself saw "cache expired" in CI. The cache reads
	// time.Now() directly, so the margin is the only lever here.
	const ttl = 200 * time.Millisecond
	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, ttl)

	preset := &Preset{
		Name:   "expiring-preset",
		Method: "POST",
	}

	testURL := "https://example.com/expiring.yaml"
	err := cache.Put(testURL, preset)
	require.NoError(t, err)

	retrieved, err := cache.Get(testURL)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)

	time.Sleep(ttl + 100*time.Millisecond)

	_, err = cache.Get(testURL)
	assert.Error(t, err)
}

func TestPresetCache_DiskPersistence(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	testURL := "https://example.com/disk-test.yaml"

	cache1 := NewPresetCache(tempDir, time.Hour)
	preset := &Preset{
		Name:   "disk-preset",
		Method: "POST",
		Body:   "test body from disk",
	}
	err := cache1.Put(testURL, preset)
	require.NoError(t, err)

	cache2 := NewPresetCache(tempDir, time.Hour)

	retrieved, err := cache2.Get(testURL)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, "disk-preset", retrieved.Name)
}

func TestPresetCache_CacheKey(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	key1 := cache.cacheKey("https://example.com/preset.yaml")
	key2 := cache.cacheKey("https://example.com/preset.yaml")
	key3 := cache.cacheKey("https://other.com/preset.yaml")

	assert.Equal(t, key1, key2)
	assert.NotEqual(t, key1, key3)
	assert.Len(t, key1, 64)
}

func TestPresetCache_Cleanup(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, 10*time.Millisecond)

	for i := range 5 {
		preset := &Preset{
			Name:   "test-preset",
			Method: "POST",
		}
		url := "https://example.com/test" + string(rune('a'+i)) + ".yaml"
		_ = cache.Put(url, preset)
	}

	assert.Len(t, cache.memory, 5)

	time.Sleep(20 * time.Millisecond)

	err := cache.Cleanup()
	require.NoError(t, err)

	assert.Empty(t, cache.memory)
}

func TestPresetCache_Clear(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	preset := &Preset{Name: "test", Method: "POST"}
	_ = cache.Put("https://example.com/test1.yaml", preset)
	_ = cache.Put("https://example.com/test2.yaml", preset)

	assert.Len(t, cache.memory, 2)

	err := cache.Clear()
	require.NoError(t, err)

	assert.Empty(t, cache.memory)
}

func TestPresetCache_Invalidate(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	testURL := "https://example.com/invalidate.yaml"
	preset := &Preset{Name: "test", Method: "POST"}
	_ = cache.Put(testURL, preset)

	_, err := cache.Get(testURL)
	require.NoError(t, err)

	cache.Invalidate(testURL)

	_, err = cache.Get(testURL)
	assert.Error(t, err)
}

func TestPresetCache_Stats(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	preset := &Preset{Name: "test", Method: "POST"}
	_ = cache.Put("https://example.com/test1.yaml", preset)
	_ = cache.Put("https://example.com/test2.yaml", preset)

	stats := cache.Stats()
	assert.Equal(t, 2, stats.MemoryEntries)
	assert.Equal(t, 2, stats.DiskEntries)
}

func TestPresetCache_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	done := make(chan bool, 10)
	for range 10 {
		go func() {
			preset := &Preset{
				Name:   "concurrent-preset",
				Method: "POST",
			}
			url := "https://example.com/concurrent.yaml"
			_ = cache.Put(url, preset)
			_, _ = cache.Get(url)
			done <- true
		}()
	}

	for range 10 {
		<-done
	}

	_, err := cache.Get("https://example.com/concurrent.yaml")
	require.NoError(t, err)
}

func TestPresetCache_DiskFilenames(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	urls := []string{
		"https://example.com/preset.yaml",
		"https://raw.githubusercontent.com/org/repo/main/file.yaml",
		"gh:org/repo/file@v1.0.0",
		"file:///path/to/local.yaml",
	}

	for _, url := range urls {
		key := cache.cacheKey(url)
		filePath := filepath.Join(tempDir, key+".yaml")

		f, err := os.Create(filePath)
		require.NoError(t, err, "Failed to create file for URL %s", url)
		f.Close()
		os.Remove(filePath)
	}
}

func TestPresetCache_DisabledWithZeroTTL(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, 0)

	preset := &Preset{
		Name:   "no-cache",
		Method: "POST",
	}

	url := "https://example.com/nocache.yaml"
	_ = cache.Put(url, preset)

	_, err := cache.Get(url)
	assert.Error(t, err)
}

func TestPresetCache_FilePermissions(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	preset := &Preset{
		Name:    "permissions-test",
		Method:  "POST",
		Version: "1.0.0",
	}

	url := "https://example.com/permissions.yaml"
	err := cache.Put(url, preset)
	require.NoError(t, err)

	key := cache.cacheKey(url)
	metaPath := filepath.Join(tempDir, key+".meta.yaml")
	presetPath := filepath.Join(tempDir, key+".yaml")

	metaInfo, err := os.Stat(metaPath)
	require.NoError(t, err)
	metaPerm := metaInfo.Mode().Perm()
	assert.Equal(t, os.FileMode(0o600), metaPerm)

	presetInfo, err := os.Stat(presetPath)
	require.NoError(t, err)
	presetPerm := presetInfo.Mode().Perm()
	assert.Equal(t, os.FileMode(0o600), presetPerm)
}

// Phase 8: Additional coverage tests for preset_cache.go

func TestPresetCache_GetFromDisk_CorruptedMetadata(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	testURL := "https://example.com/corrupted-meta.yaml"
	key := cache.cacheKey(testURL)

	metaPath := filepath.Join(tempDir, key+".meta.yaml")
	err := os.WriteFile(metaPath, []byte("not: valid: yaml: [corrupted"), 0o600)
	require.NoError(t, err)

	_, err = cache.Get(testURL)
	assert.Error(t, err, "Corrupted metadata should cause Get to fail")
}

func TestPresetCache_GetFromDisk_URLCollision(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	urlA := "https://example.com/preset-a.yaml"
	preset := &Preset{Name: "preset-a", Method: "POST"}
	err := cache.Put(urlA, preset)
	require.NoError(t, err)

	cache2 := NewPresetCache(tempDir, time.Hour)
	urlB := "https://example.com/preset-b.yaml"
	_, err = cache2.Get(urlB)
	assert.Error(t, err, "Different URL should not match cached preset")
}

func TestPresetCache_GetFromDisk_ExpiredEntry(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, time.Hour)

	testURL := "https://example.com/expired-disk.yaml"
	preset := &Preset{Name: "will-expire", Method: "POST"}
	err := cache.Put(testURL, preset)
	require.NoError(t, err)

	key := cache.cacheKey(testURL)
	metaPath := filepath.Join(tempDir, key+".meta.yaml")
	expiredMeta := "url: " + testURL + "\nfetched_at: 2020-01-01T00:00:00Z\nexpires_at: 2020-01-01T01:00:00Z\n"
	err = os.WriteFile(metaPath, []byte(expiredMeta), 0o600)
	require.NoError(t, err)

	cache2 := NewPresetCache(tempDir, time.Hour)
	_, err = cache2.Get(testURL)
	assert.Error(t, err, "Expired disk entry should cause Get to fail")
}

func TestIsMetaFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{"meta yaml file", "abc123def456.meta.yaml", true},
		{"regular yaml file", "abc123def456.yaml", false},
		{"short filename", "a.meta.yaml", true},
		{"just meta yaml", ".meta.yaml", false},
		{"json file", "abc123def456.json", false},
		{"empty string", "", false},
		{"exactly meta.yaml", "meta.yaml", false},
		{"long meta yaml", "abcdefghijk.meta.yaml", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isMetaFile(tt.filename)
			assert.Equal(t, tt.expected, result, "isMetaFile(%q)", tt.filename)
		})
	}
}
