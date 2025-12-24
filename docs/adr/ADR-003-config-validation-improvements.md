# ADR-003: Configuration Validation Improvements

**Status**: Accepted
**Date**: 2025-12-24
**Authors**: SME

## Context

Ofelia's configuration parsing has several gaps that affect user experience and maintainability:

### Problems Identified

| Problem | Impact | Example |
|---------|--------|---------|
| Unknown keys silently ignored | Typos go unnoticed | `scheduel = @hourly` works (no error) |
| No value range validation | Invalid values accepted | `smtp-port = 99999` passes |
| Deprecated detection by VALUE | Zero-value deprecations missed | `poll-interval = 0` not flagged |
| Mixed validation systems | Maintenance burden | Custom `config/validator.go` + struct tags |

### Why Current Approach is Insufficient

The `mapstructure.WeakDecode()` function by design:
- Does NOT report unknown keys (they're silently dropped)
- Does NOT distinguish "not set" from "set to zero value"
- Does NOT validate value ranges

This means:
- Users with typos get no feedback
- Deprecated options set to `0` are never detected
- Invalid configurations may run with silent fallback behavior

## Decision

Implement a two-layer validation approach using:

1. **mapstructure Metadata** - for key presence and unknown key detection
2. **go-playground/validator/v10** - for struct value validation

### Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         INI/Label Input                              │
└─────────────────────────────┬───────────────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│   Layer 1: mapstructure.Decode() with DecoderConfig                  │
│   ├─ Metadata.Keys → track ALL keys that were set                    │
│   ├─ Metadata.Unused → detect unknown/typo keys                      │
│   └─ WeaklyTypedInput: true → permissive type coercion               │
└─────────────────────────────┬───────────────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│   Layer 2: go-playground/validator                                   │
│   ├─ validate:"required" → required fields                           │
│   ├─ validate:"gte=0,lte=65535" → range validation                   │
│   ├─ validate:"oneof=debug info warning error" → enum validation     │
│   └─ validate:"url|email" → format validation                        │
└─────────────────────────────┬───────────────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│   Deprecation Detection (enhanced)                                   │
│   ├─ Check Metadata.Keys for deprecated key PRESENCE                 │
│   └─ Trigger warning regardless of value (including zero)            │
└─────────────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

1. **DecodeResult struct** tracks used and unused keys:
   ```go
   type DecodeResult struct {
       UsedKeys   map[string]bool // Keys present in input
       UnusedKeys []string        // Keys not matching any struct field
   }
   ```

2. **Deprecation detection by key presence**, not just value - enables detecting `poll-interval = 0`

3. **Levenshtein distance** for "did you mean?" suggestions on unknown keys

4. **Warnings, not errors** for unknown keys - maintains backwards compatibility

## Consequences

### Positive

1. **Better User Experience**:
   - Immediate feedback on typos with suggestions
   - Clear validation error messages
   - Deprecation warnings for all deprecated keys (not just non-zero values)

2. **Reduced Support Burden**:
   - Fewer "silent failure" bug reports
   - Self-documenting validation via struct tags
   - Easier configuration debugging

3. **Maintainability**:
   - Single source of truth for validation rules (struct tags)
   - Well-tested external library (go-playground/validator)
   - Consistent validation patterns across codebase

4. **Compatibility**:
   - mapstructure already in use (no new dependency for decoding)
   - go-playground/validator is mature, widely used
   - Existing config files continue to work

### Negative

1. **New Dependency**: Adds `github.com/go-playground/validator/v10` (~2MB binary size)

2. **Two validation systems temporarily**: `config/validator.go` (existing) coexists with new validator

3. **Unknown keys now generate warnings**: May surprise users with existing typos

### Scope Limitations

- Unknown key detection currently applies to `[global]` and `[docker]` sections only
- Job sections (`[job-exec]`, etc.) do not yet report unknown keys

## Alternatives Considered

### 1. mapstructure ErrorUnused Only
**Rejected**: Only detects unknown keys, no value validation.

### 2. Custom Validation Framework
**Rejected**: Already have one (`config/validator.go`), it's maintenance burden.

### 3. JSON Schema Validation
**Rejected**: INI format not directly convertible, adds complexity.

### 4. Keep Current System
**Rejected**: User experience issues (silent typos, missed deprecations) are significant pain points.

## References

- mapstructure Metadata: https://pkg.go.dev/github.com/mitchellh/mapstructure#Metadata
- go-playground/validator: https://pkg.go.dev/github.com/go-playground/validator/v10
- Implementation: `cli/config_decode.go`, `cli/config_validate.go`, `cli/deprecations.go`
