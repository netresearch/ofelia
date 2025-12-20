package middlewares

import (
	"os"
	"path/filepath"
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

	tempDir := t.TempDir()
	cache := NewPresetCache(tempDir, 10*time.Millisecond)

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

	time.Sleep(20 * time.Millisecond)

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

	for i := 0; i < 5; i++ {
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
	for i := 0; i < 10; i++ {
		go func(n int) {
			preset := &Preset{
				Name:   "concurrent-preset",
				Method: "POST",
			}
			url := "https://example.com/concurrent.yaml"
			_ = cache.Put(url, preset)
			_, _ = cache.Get(url)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
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
