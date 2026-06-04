// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-viper/mapstructure/v2"
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
		DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
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

// weakDecodeConsistent decodes input into output using the same options as
// decodeWithMetadata (case-insensitive matching, weak typing) but accepts any
// input type. This is used for Docker label decoding where the input shape is
// map[string]map[string]any rather than flat map[string]any.
func weakDecodeConsistent(input, output any) error {
	config := &mapstructure.DecoderConfig{
		Result:           output,
		WeaklyTypedInput: true,
		TagName:          "mapstructure",
		MatchName:        caseInsensitiveMatch,
		DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
	}

	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return fmt.Errorf("create decoder: %w", err)
	}

	if err := decoder.Decode(input); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
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
		if !field.IsExported() {
			continue
		}
		if k := mapstructureKeyForField(field); k != "" {
			keys = append(keys, k)
		} else if field.Anonymous || hasSquashTag(field) {
			keys = append(keys, extractMapstructureKeysFromType(field.Type)...)
		}
	}
	return keys
}

// mapstructureKeyForField returns the configuration key name for a struct field,
// or "" when the field should be skipped or expanded (embedded/squashed structs).
// Priority: explicit mapstructure tag → lowercase field name. Returns "" for
// ignored ("-") fields and for anonymous/squashed fields (handled by caller).
func mapstructureKeyForField(field reflect.StructField) string {
	// Embedded/squashed structs are expanded by the caller.
	if field.Anonymous || hasSquashTag(field) {
		return ""
	}
	tag := field.Tag.Get("mapstructure")
	if tag == "-" {
		return ""
	}
	if tag != "" {
		if name := strings.SplitN(tag, ",", 2)[0]; name != "" && name != "-" {
			return name
		}
	}
	// No mapstructure tag or empty name - use lowercase field name.
	return strings.ToLower(field.Name)
}

// hasSquashTag checks if a struct field has the mapstructure ",squash" tag option.
// The squash option tells mapstructure to flatten the embedded struct's fields into
// the parent struct during decoding, rather than nesting them under the field name.
func hasSquashTag(field reflect.StructField) bool {
	tag := field.Tag.Get("mapstructure")
	return strings.Contains(tag, "squash")
}
