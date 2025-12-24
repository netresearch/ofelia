# ADR-003: Configuration Validation Improvements

**Status**: Proposed
**Date**: 2025-12-24
**Authors**: SME

## Context

Ofelia's configuration parsing has several gaps that affect user experience and maintainability:

### Current State

1. **INI/Label Parsing Flow**:
   ```
   INI File → go-ini/ini → sectionToMap() → mapstructure.WeakDecode() → Config struct
   Labels   → Docker API → parseJobLabels() → mapstructure.WeakDecode() → Config struct
   ```

2. **Problems Identified**:

   | Problem | Impact | Example |
   |---------|--------|---------|
   | Unknown keys silently ignored | Typos go unnoticed | `scheduel = @hourly` works (no error) |
   | No value range validation | Invalid values accepted | `smtp-port = 99999` passes |
   | Deprecated detection by VALUE | Zero-value deprecations missed | `poll-interval = 0` not flagged |
   | Mixed validation systems | Maintenance burden | Custom `config/validator.go` + struct tags |

3. **Current Validation**:
   - `mapstructure.WeakDecode()` - permissive type coercion, silently ignores unknown keys
   - `config/validator.go` - custom reflection-based validation, runs after decode
   - No integration between unknown key detection and validation

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

### Implementation Details

#### 1. New Decoder Function

```go
// cli/config_decode.go

type DecodeResult struct {
    UsedKeys   map[string]bool // Keys that were present in input
    UnusedKeys []string        // Keys not matching any struct field
}

func decodeWithMetadata(input map[string]interface{}, output interface{}) (*DecodeResult, error) {
    var metadata mapstructure.Metadata

    config := &mapstructure.DecoderConfig{
        Result:           output,
        Metadata:         &metadata,
        WeaklyTypedInput: true,
        TagName:          "mapstructure",
        // ErrorUnused: true would fail; we want to collect them instead
    }

    decoder, err := mapstructure.NewDecoder(config)
    if err != nil {
        return nil, err
    }

    if err := decoder.Decode(input); err != nil {
        return nil, err
    }

    result := &DecodeResult{
        UsedKeys:   make(map[string]bool),
        UnusedKeys: metadata.Unused,
    }

    // Track which keys were present
    for _, key := range metadata.Keys {
        result.UsedKeys[key] = true
    }

    return result, nil
}
```

#### 2. Validator Integration

```go
// cli/config_validate.go

import "github.com/go-playground/validator/v10"

var validate = validator.New()

func init() {
    // Register custom validators
    validate.RegisterValidation("cron", validateCron)
    validate.RegisterValidation("dockerimage", validateDockerImage)
}

func validateConfig(cfg *Config) error {
    return validate.Struct(cfg)
}
```

#### 3. Struct Tag Updates

```go
// cli/config.go - DockerConfig example

type DockerConfig struct {
    // ConfigPollInterval controls how often to check for INI config file changes.
    ConfigPollInterval time.Duration `mapstructure:"config-poll-interval" validate:"gte=0" default:"10s"`

    // UseEvents enables Docker event-based container detection.
    UseEvents bool `mapstructure:"events" default:"true"`

    // DockerPollInterval enables periodic polling for container changes.
    DockerPollInterval time.Duration `mapstructure:"docker-poll-interval" validate:"gte=0" default:"0"`

    // PollingFallback auto-enables container polling if event subscription fails.
    PollingFallback time.Duration `mapstructure:"polling-fallback" validate:"gte=0" default:"10s"`

    // Deprecated fields - detected by key presence, not value
    PollInterval   time.Duration `mapstructure:"poll-interval" validate:"gte=0"`
    DisablePolling bool          `mapstructure:"no-poll"`
}
```

#### 4. Enhanced Deprecation Detection

```go
// cli/deprecations.go - Updated CheckFunc signature

type Deprecation struct {
    Option         string
    Replacement    string
    RemovalVersion string
    Message        string

    // CheckFunc now receives decode metadata for presence-based detection
    CheckFunc   func(cfg *Config, usedKeys map[string]bool) bool
    MigrateFunc func(cfg *Config)
}

// Example: poll-interval deprecation
{
    Option:         "poll-interval",
    Replacement:    "config-poll-interval and docker-poll-interval",
    RemovalVersion: "v1.0.0",
    CheckFunc: func(cfg *Config, usedKeys map[string]bool) bool {
        // Detect by presence, not value!
        return usedKeys["poll-interval"]
    },
    MigrateFunc: func(cfg *Config) {
        // ... migration logic
    },
}
```

#### 5. Unknown Key Reporting

```go
// cli/config.go - BuildFromFile

func BuildFromFile(filename string, logger core.Logger) (*Config, error) {
    // ... INI loading ...

    result, err := decodeWithMetadata(sectionToMap(sec), &c.Global)
    if err != nil {
        return nil, err
    }

    // Report unknown keys
    for _, key := range result.UnusedKeys {
        logger.Warningf("Unknown configuration key '%s' in [global] section (typo?)", key)
    }

    // Validate struct
    if err := validateConfig(c); err != nil {
        return nil, fmt.Errorf("validation failed: %w", err)
    }

    // Check deprecations with key presence info
    checkDeprecationsWithMetadata(c, result.UsedKeys)

    // ...
}
```

### Validation Tag Reference

| Tag | Purpose | Example |
|-----|---------|---------|
| `validate:"required"` | Field must be non-zero | `Schedule string` |
| `validate:"gte=0,lte=65535"` | Numeric range | `SMTPPort int` |
| `validate:"oneof=debug info warning error"` | Enum values | `LogLevel string` |
| `validate:"url"` | Valid URL format | `SlackWebhook string` |
| `validate:"email"` | Valid email format | `EmailFrom string` |
| `validate:"min=1,max=255"` | String length | `Container string` |
| Custom: `validate:"cron"` | Cron expression | `Schedule string` |
| Custom: `validate:"dockerimage"` | Docker image ref | `Image string` |

### Error Messages

```
Unknown configuration keys detected:
  - [global] scheduel (did you mean 'schedule'?)
  - [job-exec "backup"] comand (did you mean 'command'?)

Validation errors:
  - [global] smtp-port: must be between 1 and 65535 (got: 99999)
  - [job-exec "backup"] schedule: required field is empty

Deprecation warnings:
  - 'poll-interval' is deprecated (present in config), use 'config-poll-interval' instead
```

## Consequences

### Positive

1. **Better User Experience**:
   - Immediate feedback on typos
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
   - Existing config files continue to work (warnings, not errors for unknown keys)

### Negative

1. **New Dependency**:
   - Adds `github.com/go-playground/validator/v10`
   - ~2MB additional binary size

2. **Migration Effort**:
   - Must update all struct definitions with validation tags
   - Must update all `mapstructure.WeakDecode()` calls
   - Tests need updating

3. **Strict vs Lenient Behavior**:
   - Unknown keys now generate warnings (may surprise users)
   - Decision: Warnings only (not errors) for backwards compatibility
   - Can make strict mode opt-in via `enable-strict-validation: true`

### Neutral

1. **Two validation systems temporarily**:
   - `config/validator.go` (existing) - phased out
   - `go-playground/validator` (new) - primary going forward
   - Migration path: gradual replacement over time

## Alternatives Considered

### 1. mapstructure ErrorUnused Only

**Rejected**: Only detects unknown keys, no value validation. Would still need custom validation.

### 2. Custom Validation Framework

**Rejected**: Already have one (`config/validator.go`), it's maintenance burden. go-playground/validator is battle-tested.

### 3. JSON Schema Validation

**Rejected**: INI format not directly convertible, adds complexity without benefit for this use case.

### 4. Keep Current System

**Rejected**: User experience issues (silent typos, missed deprecations) are significant pain points.

## Implementation Plan

### Phase 1: Core Infrastructure (This PR)

1. Add `go-playground/validator/v10` dependency
2. Create `cli/config_decode.go` with `decodeWithMetadata()`
3. Create `cli/config_validate.go` with validator setup and custom validators
4. Update deprecation `CheckFunc` signature to accept `usedKeys`
5. Add comprehensive tests

### Phase 2: Integration (This PR)

6. Update `sectionToMap()` calls to use new decoder
7. Add validation tags to all config structs
8. Integrate validation into `BuildFromFile()` and `BuildFromString()`
9. Add unknown key warnings to logger
10. Update `dockerLabelsUpdate()` for label parsing
11. Add Levenshtein distance for "did you mean?" suggestions

### Phase 3: Cleanup (Future)

12. Deprecate `config/validator.go` custom validators
13. Migrate remaining manual validation to struct tags
14. Add unknown key detection to job section parsing

## References

- mapstructure Metadata: https://pkg.go.dev/github.com/mitchellh/mapstructure#Metadata
- go-playground/validator: https://pkg.go.dev/github.com/go-playground/validator/v10
- Current deprecation system: `cli/deprecations.go`
- Current validation: `config/validator.go`

## Test Plan

1. **Unknown Key Detection**:
   - Typo in global section → warning logged
   - Typo in docker section → warning logged
   - Typo in job section → no warning (to be improved in Phase 3)

2. **Value Validation**:
   - Invalid port number → error
   - Invalid cron expression → error
   - Invalid enum value → error

3. **Deprecation Detection**:
   - `poll-interval = 30s` → warning (value-based, existing)
   - `poll-interval = 0` → warning (presence-based, NEW)
   - No poll-interval key → no warning

4. **Backwards Compatibility**:
   - Existing valid configs continue to work
   - Only new validation errors/warnings, no behavior changes
