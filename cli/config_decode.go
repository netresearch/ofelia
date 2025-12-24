package cli

import (
	"fmt"
	"strings"

	"github.com/mitchellh/mapstructure"
)

// DecodeResult contains metadata from the decoding process
type DecodeResult struct {
	// UsedKeys contains all keys that were present in the input
	UsedKeys map[string]bool

	// UnusedKeys contains keys that were in input but not matched to struct fields
	UnusedKeys []string
}

// decodeWithMetadata decodes input into output while tracking key metadata.
// This enables detection of unknown keys (typos) and presence-based deprecation checks.
func decodeWithMetadata(input map[string]interface{}, output interface{}) (*DecodeResult, error) {
	var metadata mapstructure.Metadata

	config := &mapstructure.DecoderConfig{
		Result:           output,
		Metadata:         &metadata,
		WeaklyTypedInput: true,
		TagName:          "mapstructure",
		MatchName:        caseInsensitiveMatch,
	}

	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return nil, fmt.Errorf("create decoder: %w", err)
	}

	if err := decoder.Decode(input); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	result := &DecodeResult{
		UsedKeys:   make(map[string]bool),
		UnusedKeys: metadata.Unused,
	}

	// Track which keys were present in the input
	for _, key := range metadata.Keys {
		result.UsedKeys[normalizeKey(key)] = true
	}

	return result, nil
}

// caseInsensitiveMatch matches map keys to struct fields case-insensitively
func caseInsensitiveMatch(mapKey, fieldName string) bool {
	return strings.EqualFold(normalizeKey(mapKey), normalizeKey(fieldName))
}

// normalizeKey normalizes a configuration key for comparison
// Handles both kebab-case (config files) and underscores (mapstructure tags)
func normalizeKey(key string) string {
	// Convert to lowercase and normalize separators
	k := strings.ToLower(key)
	k = strings.ReplaceAll(k, "-", "")
	k = strings.ReplaceAll(k, "_", "")
	return k
}

// collectInputKeys collects all keys from a map for key presence tracking
func collectInputKeys(input map[string]interface{}) map[string]bool {
	keys := make(map[string]bool)
	for k := range input {
		keys[normalizeKey(k)] = true
	}
	return keys
}

// mergeUsedKeys merges multiple DecodeResult.UsedKeys maps into one
func mergeUsedKeys(results ...*DecodeResult) map[string]bool {
	merged := make(map[string]bool)
	for _, r := range results {
		if r == nil {
			continue
		}
		for k, v := range r.UsedKeys {
			if v {
				merged[k] = true
			}
		}
	}
	return merged
}
