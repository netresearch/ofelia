package cli

import (
	"fmt"
	"reflect"
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
func decodeWithMetadata(input map[string]any, output any) (*DecodeResult, error) {
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
func collectInputKeys(input map[string]any) map[string]bool {
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

// extractMapstructureKeys extracts all configuration keys that mapstructure will
// recognize for the given struct value. It returns keys from mapstructure tags,
// or lowercase field names when no tag is specified. This is used to build the
// list of known keys for "did you mean?" suggestions when unknown keys are detected.
func extractMapstructureKeys(v any) []string {
	return extractMapstructureKeysFromType(reflect.TypeOf(v))
}

// extractMapstructureKeysFromType recursively extracts mapstructure keys from a
// reflect.Type. It handles pointer types, embedded structs (both anonymous fields
// and those with the ",squash" tag option), and fields without explicit tags.
// For embedded/squashed structs, keys are collected recursively and flattened.
func extractMapstructureKeysFromType(t reflect.Type) []string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	keys := make([]string, 0)
	for field := range t.Fields() {
		field := field

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Handle embedded structs (squash)
		if field.Anonymous || hasSquashTag(field) {
			embedded := extractMapstructureKeysFromType(field.Type)
			keys = append(keys, embedded...)
			continue
		}

		// Get mapstructure tag
		tag := field.Tag.Get("mapstructure")

		// Skip fields explicitly marked to ignore
		if tag == "-" {
			continue
		}

		// Parse tag (handle "name,omitempty" format)
		if tag != "" {
			parts := strings.Split(tag, ",")
			name := parts[0]
			if name != "" && name != "-" {
				keys = append(keys, name)
				continue
			}
		}

		// No mapstructure tag or empty name - use lowercase field name
		// mapstructure matches by lowercase field name when no tag is specified
		keys = append(keys, strings.ToLower(field.Name))
	}

	return keys
}

// hasSquashTag checks if a struct field has the mapstructure ",squash" tag option.
// The squash option tells mapstructure to flatten the embedded struct's fields into
// the parent struct during decoding, rather than nesting them under the field name.
func hasSquashTag(field reflect.StructField) bool {
	tag := field.Tag.Get("mapstructure")
	return strings.Contains(tag, "squash")
}
