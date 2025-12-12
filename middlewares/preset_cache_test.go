package middlewares

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"
)

type SuitePresetCache struct {
	BaseSuite
	tempDir string
}

var _ = Suite(&SuitePresetCache{})

func (s *SuitePresetCache) SetUpTest(c *C) {
	s.BaseSuite.SetUpTest(c)
	s.tempDir = c.MkDir()
}

func (s *SuitePresetCache) TestNewPresetCache(c *C) {
	cache := NewPresetCache(s.tempDir, time.Hour)

	c.Assert(cache, NotNil)
	c.Assert(cache.ttl, Equals, time.Hour)
	c.Assert(cache.cacheDir, Equals, s.tempDir)
	c.Assert(cache.memory, NotNil)
}

func (s *SuitePresetCache) TestNewPresetCache_DefaultDir(c *C) {
	cache := NewPresetCache("", time.Hour)

	c.Assert(cache, NotNil)
	c.Assert(cache.cacheDir, Not(Equals), "")
}

func (s *SuitePresetCache) TestPresetCache_PutGet_Memory(c *C) {
	cache := NewPresetCache(s.tempDir, time.Hour)

	preset := &Preset{
		Name:   "test-preset",
		Method: "POST",
		Body:   "test body",
	}

	testURL := "https://example.com/test.yaml"
	err := cache.Put(testURL, preset)
	c.Assert(err, IsNil)

	// Get from cache
	retrieved, err := cache.Get(testURL)
	c.Assert(err, IsNil)
	c.Assert(retrieved, NotNil)
	c.Assert(retrieved.Name, Equals, "test-preset")
}

func (s *SuitePresetCache) TestPresetCache_Get_NotFound(c *C) {
	cache := NewPresetCache(s.tempDir, time.Hour)

	_, err := cache.Get("https://nonexistent.com/preset.yaml")
	c.Assert(err, NotNil)
}

func (s *SuitePresetCache) TestPresetCache_Expiration(c *C) {
	cache := NewPresetCache(s.tempDir, 10*time.Millisecond)

	preset := &Preset{
		Name:   "expiring-preset",
		Method: "POST",
	}

	testURL := "https://example.com/expiring.yaml"
	err := cache.Put(testURL, preset)
	c.Assert(err, IsNil)

	// Should exist immediately
	retrieved, err := cache.Get(testURL)
	c.Assert(err, IsNil)
	c.Assert(retrieved, NotNil)

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Should be expired
	_, err = cache.Get(testURL)
	c.Assert(err, NotNil)
}

func (s *SuitePresetCache) TestPresetCache_DiskPersistence(c *C) {
	testURL := "https://example.com/disk-test.yaml"

	// First cache instance - save to disk
	cache1 := NewPresetCache(s.tempDir, time.Hour)
	preset := &Preset{
		Name:   "disk-preset",
		Method: "POST",
		Body:   "test body from disk",
	}
	err := cache1.Put(testURL, preset)
	c.Assert(err, IsNil)

	// Second cache instance - should load from disk
	cache2 := NewPresetCache(s.tempDir, time.Hour)

	// Memory cache is empty in new instance, but disk should have it
	retrieved, err := cache2.Get(testURL)
	c.Assert(err, IsNil)
	c.Assert(retrieved, NotNil)
	c.Assert(retrieved.Name, Equals, "disk-preset")
}

func (s *SuitePresetCache) TestPresetCache_CacheKey(c *C) {
	cache := NewPresetCache(s.tempDir, time.Hour)

	key1 := cache.cacheKey("https://example.com/preset.yaml")
	key2 := cache.cacheKey("https://example.com/preset.yaml")
	key3 := cache.cacheKey("https://other.com/preset.yaml")

	// Same URL should produce same key
	c.Assert(key1, Equals, key2)

	// Different URL should produce different key
	c.Assert(key1, Not(Equals), key3)

	// Key should be SHA256 hex (64 chars)
	c.Assert(len(key1), Equals, 64)
}

func (s *SuitePresetCache) TestPresetCache_Cleanup(c *C) {
	cache := NewPresetCache(s.tempDir, 10*time.Millisecond)

	// Add multiple entries
	for i := 0; i < 5; i++ {
		preset := &Preset{
			Name:   "test-preset",
			Method: "POST",
		}
		url := "https://example.com/test" + string(rune('a'+i)) + ".yaml"
		_ = cache.Put(url, preset)
	}

	c.Assert(len(cache.memory), Equals, 5)

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Cleanup
	err := cache.Cleanup()
	c.Assert(err, IsNil)

	c.Assert(len(cache.memory), Equals, 0)
}

func (s *SuitePresetCache) TestPresetCache_Clear(c *C) {
	cache := NewPresetCache(s.tempDir, time.Hour)

	// Add entries
	preset := &Preset{Name: "test", Method: "POST"}
	_ = cache.Put("https://example.com/test1.yaml", preset)
	_ = cache.Put("https://example.com/test2.yaml", preset)

	c.Assert(len(cache.memory), Equals, 2)

	// Clear
	err := cache.Clear()
	c.Assert(err, IsNil)

	c.Assert(len(cache.memory), Equals, 0)
}

func (s *SuitePresetCache) TestPresetCache_Invalidate(c *C) {
	cache := NewPresetCache(s.tempDir, time.Hour)

	testURL := "https://example.com/invalidate.yaml"
	preset := &Preset{Name: "test", Method: "POST"}
	_ = cache.Put(testURL, preset)

	// Should exist
	_, err := cache.Get(testURL)
	c.Assert(err, IsNil)

	// Invalidate
	cache.Invalidate(testURL)

	// Should not exist
	_, err = cache.Get(testURL)
	c.Assert(err, NotNil)
}

func (s *SuitePresetCache) TestPresetCache_Stats(c *C) {
	cache := NewPresetCache(s.tempDir, time.Hour)

	preset := &Preset{Name: "test", Method: "POST"}
	_ = cache.Put("https://example.com/test1.yaml", preset)
	_ = cache.Put("https://example.com/test2.yaml", preset)

	stats := cache.Stats()
	c.Assert(stats.MemoryEntries, Equals, 2)
	c.Assert(stats.DiskEntries, Equals, 2)
}

// Standard Go testing for concurrent access

func TestPresetCache_ConcurrentAccess(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "preset-cache-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewPresetCache(tempDir, time.Hour)

	// Concurrent writes
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

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic and should have valid state
	_, err = cache.Get("https://example.com/concurrent.yaml")
	if err != nil {
		t.Error("Expected key to exist after concurrent access")
	}
}

func TestPresetCache_DiskFilenames(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "preset-cache-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewPresetCache(tempDir, time.Hour)

	// Test various URL patterns
	urls := []string{
		"https://example.com/preset.yaml",
		"https://raw.githubusercontent.com/org/repo/main/file.yaml",
		"gh:org/repo/file@v1.0.0",
		"file:///path/to/local.yaml",
	}

	for _, url := range urls {
		key := cache.cacheKey(url)

		// Key should be safe for filesystem
		filePath := filepath.Join(tempDir, key+".yaml")

		// Should be able to create file with this name
		f, err := os.Create(filePath)
		if err != nil {
			t.Errorf("Failed to create file for URL %s: %v", url, err)
			continue
		}
		f.Close()

		// Cleanup
		os.Remove(filePath)
	}
}

func TestPresetCache_DisabledWithZeroTTL(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "preset-cache-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewPresetCache(tempDir, 0)

	preset := &Preset{
		Name:   "no-cache",
		Method: "POST",
	}

	url := "https://example.com/nocache.yaml"
	_ = cache.Put(url, preset)

	// With zero TTL, cache should effectively be disabled
	// (entry expires immediately)
	_, err = cache.Get(url)
	if err == nil {
		t.Error("Zero TTL cache should not return entries")
	}
}

func TestPresetCache_FilePermissions(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "preset-cache-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewPresetCache(tempDir, time.Hour)

	preset := &Preset{
		Name:    "permissions-test",
		Method:  "POST",
		Version: "1.0.0",
	}

	url := "https://example.com/permissions.yaml"
	err = cache.Put(url, preset)
	if err != nil {
		t.Fatalf("Failed to put preset: %v", err)
	}

	// Find the cached files
	key := cache.cacheKey(url)
	metaPath := filepath.Join(tempDir, key+".meta.yaml")
	presetPath := filepath.Join(tempDir, key+".yaml")

	// Check metadata file permissions
	metaInfo, err := os.Stat(metaPath)
	if err != nil {
		t.Fatalf("Failed to stat metadata file: %v", err)
	}
	metaPerm := metaInfo.Mode().Perm()
	if metaPerm != 0o600 {
		t.Errorf("Expected metadata file permissions 0600, got %04o", metaPerm)
	}

	// Check preset file permissions
	presetInfo, err := os.Stat(presetPath)
	if err != nil {
		t.Fatalf("Failed to stat preset file: %v", err)
	}
	presetPerm := presetInfo.Mode().Perm()
	if presetPerm != 0o600 {
		t.Errorf("Expected preset file permissions 0600, got %04o", presetPerm)
	}
}
