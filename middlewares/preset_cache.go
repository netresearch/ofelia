package middlewares

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const yamlExt = ".yaml"

// PresetCache provides caching for remote presets
type PresetCache struct {
	cacheDir string
	ttl      time.Duration
	memory   map[string]*cachedPreset
	mu       sync.RWMutex
}

// cachedPreset holds a preset with its expiration time
type cachedPreset struct {
	preset    *Preset
	expiresAt time.Time
}

// cacheMetadata is stored alongside cached preset files
type cacheMetadata struct {
	URL       string    `yaml:"url"`
	FetchedAt time.Time `yaml:"fetched_at"` //nolint:tagliatelle // snake_case is idiomatic for YAML
	ExpiresAt time.Time `yaml:"expires_at"` //nolint:tagliatelle // snake_case is idiomatic for YAML
	Version   string    `yaml:"version,omitempty"`
}

// NewPresetCache creates a new preset cache
func NewPresetCache(cacheDir string, ttl time.Duration) *PresetCache {
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "ofelia", "presets")
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create preset cache directory: %v\n", err)
	}

	return &PresetCache{
		cacheDir: cacheDir,
		ttl:      ttl,
		memory:   make(map[string]*cachedPreset),
	}
}

// cacheKey generates a unique key for a URL
func (c *PresetCache) cacheKey(url string) string {
	hash := sha256.Sum256([]byte(url))
	return hex.EncodeToString(hash[:])
}

// Get retrieves a preset from cache
func (c *PresetCache) Get(url string) (*Preset, error) {
	key := c.cacheKey(url)

	// Check memory cache first
	c.mu.RLock()
	if cached, ok := c.memory[key]; ok && time.Now().Before(cached.expiresAt) {
		c.mu.RUnlock()
		return cached.preset, nil
	}
	c.mu.RUnlock()

	// Check disk cache
	preset, err := c.getFromDisk(key, url)
	if err != nil {
		return nil, err
	}

	// Store in memory cache
	c.mu.Lock()
	c.memory[key] = &cachedPreset{
		preset:    preset,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return preset, nil
}

// Put stores a preset in cache
func (c *PresetCache) Put(url string, preset *Preset) error {
	key := c.cacheKey(url)
	expiresAt := time.Now().Add(c.ttl)

	// Store in memory
	c.mu.Lock()
	c.memory[key] = &cachedPreset{
		preset:    preset,
		expiresAt: expiresAt,
	}
	c.mu.Unlock()

	// Store on disk
	return c.putToDisk(key, url, preset, expiresAt)
}

// getFromDisk retrieves a preset from disk cache
func (c *PresetCache) getFromDisk(key, url string) (*Preset, error) {
	metaPath := filepath.Join(c.cacheDir, key+".meta.yaml")
	presetPath := filepath.Join(c.cacheDir, key+".yaml")

	// Read metadata
	metaData, err := os.ReadFile(metaPath) // #nosec G304 -- path is constructed from controlled cache directory
	if err != nil {
		return nil, fmt.Errorf("cache miss: %w", err)
	}

	var meta cacheMetadata
	if err := yaml.Unmarshal(metaData, &meta); err != nil {
		return nil, fmt.Errorf("invalid cache metadata: %w", err)
	}

	// Check expiration
	if time.Now().After(meta.ExpiresAt) {
		// Clean up expired cache files
		_ = os.Remove(metaPath)
		_ = os.Remove(presetPath)
		return nil, fmt.Errorf("cache expired")
	}

	// Verify URL matches
	if meta.URL != url {
		return nil, fmt.Errorf("cache key collision")
	}

	// Read preset
	presetData, err := os.ReadFile(presetPath) // #nosec G304 -- path is constructed from controlled cache directory
	if err != nil {
		return nil, fmt.Errorf("read cached preset: %w", err)
	}

	preset, err := ParsePreset(presetData)
	if err != nil {
		return nil, fmt.Errorf("parse cached preset: %w", err)
	}

	return preset, nil
}

// putToDisk stores a preset on disk
func (c *PresetCache) putToDisk(key, url string, preset *Preset, expiresAt time.Time) error {
	metaPath := filepath.Join(c.cacheDir, key+".meta.yaml")
	presetPath := filepath.Join(c.cacheDir, key+".yaml")

	// Write metadata
	meta := cacheMetadata{
		URL:       url,
		FetchedAt: time.Now(),
		ExpiresAt: expiresAt,
		Version:   preset.Version,
	}

	metaData, err := yaml.Marshal(&meta)
	if err != nil {
		return fmt.Errorf("marshal cache metadata: %w", err)
	}

	if err := os.WriteFile(metaPath, metaData, 0o600); err != nil {
		return fmt.Errorf("write cache metadata: %w", err)
	}

	// Write preset
	presetData, err := yaml.Marshal(preset)
	if err != nil {
		return fmt.Errorf("marshal preset: %w", err)
	}

	if err := os.WriteFile(presetPath, presetData, 0o600); err != nil {
		return fmt.Errorf("write cached preset: %w", err)
	}

	return nil
}

// Invalidate removes a preset from cache
func (c *PresetCache) Invalidate(url string) {
	key := c.cacheKey(url)

	// Remove from memory
	c.mu.Lock()
	delete(c.memory, key)
	c.mu.Unlock()

	// Remove from disk
	metaPath := filepath.Join(c.cacheDir, key+".meta.yaml")
	presetPath := filepath.Join(c.cacheDir, key+".yaml")
	_ = os.Remove(metaPath)
	_ = os.Remove(presetPath)
}

// Clear removes all cached presets
func (c *PresetCache) Clear() error {
	// Clear memory
	c.mu.Lock()
	c.memory = make(map[string]*cachedPreset)
	c.mu.Unlock()

	// Clear disk
	entries, err := os.ReadDir(c.cacheDir)
	if err != nil {
		return fmt.Errorf("read cache directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(c.cacheDir, entry.Name())
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove cached file %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// Cleanup removes expired entries from cache
func (c *PresetCache) Cleanup() error {
	// Cleanup memory
	c.mu.Lock()
	now := time.Now()
	for key, cached := range c.memory {
		if now.After(cached.expiresAt) {
			delete(c.memory, key)
		}
	}
	c.mu.Unlock()

	// Cleanup disk
	entries, err := os.ReadDir(c.cacheDir)
	if err != nil {
		return fmt.Errorf("read cache directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != yamlExt {
			continue
		}

		// Skip preset files, only process metadata
		if !isMetaFile(entry.Name()) {
			continue
		}

		metaPath := filepath.Join(c.cacheDir, entry.Name())
		metaData, err := os.ReadFile(metaPath) // #nosec G304 -- path is constructed from controlled cache directory
		if err != nil {
			continue
		}

		var meta cacheMetadata
		if err := yaml.Unmarshal(metaData, &meta); err != nil {
			// Invalid metadata, remove
			_ = os.Remove(metaPath)
			continue
		}

		if now.After(meta.ExpiresAt) {
			// Remove expired files
			_ = os.Remove(metaPath)
			presetPath := metaPath[:len(metaPath)-len(".meta.yaml")] + ".yaml"
			_ = os.Remove(presetPath)
		}
	}

	return nil
}

// isMetaFile checks if a filename is a metadata file
func isMetaFile(name string) bool {
	return len(name) > 10 && name[len(name)-10:] == ".meta.yaml"
}

// Stats returns cache statistics
func (c *PresetCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := CacheStats{
		MemoryEntries: len(c.memory),
	}

	// Count disk entries
	entries, err := os.ReadDir(c.cacheDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && isMetaFile(entry.Name()) {
				stats.DiskEntries++
			}
		}
	}

	return stats
}

// CacheStats holds cache statistics
type CacheStats struct {
	MemoryEntries int
	DiskEntries   int
}
