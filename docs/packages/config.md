# Config Package

**Package**: `config`
**Path**: `/config/`
**Purpose**: Configuration validation, input sanitization, and security enforcement

## Overview

The config package provides comprehensive validation and sanitization for Ofelia configuration inputs, protecting against injection attacks, path traversal, and other security vulnerabilities. It combines field-level validation with security-focused input sanitization to ensure safe configuration processing.

## Key Components

### 1. Validator

Configuration validation with field-level rules and type checking.

```go
type Validator struct {
    errors ValidationErrors
}

type ValidationError struct {
    Field   string
    Value   interface{}
    Message string
}
```

**Features**:
- Field-level validation with error accumulation
- Type-specific validation (string, int, slice)
- Range and length validation
- Format validation (URL, email, cron)
- Enum validation for allowed values

**Creation and Usage**:
```go
// Create validator
validator := config.NewValidator()

// Perform validations
validator.ValidateRequired("schedule", job.Schedule)
validator.ValidateEmail("email-to", job.EmailTo)
validator.ValidateCronExpression("schedule", job.Schedule)

// Check for errors
if validator.HasErrors() {
    for _, err := range validator.Errors() {
        log.Printf("Validation error: %v", err)
    }
}
```

### 2. Sanitizer

Security-focused input sanitization protecting against multiple attack vectors.

```go
type Sanitizer struct {
    sqlInjectionPattern   *regexp.Regexp
    shellInjectionPattern *regexp.Regexp
    pathTraversalPattern  *regexp.Regexp
    ldapInjectionPattern  *regexp.Regexp
}
```

**Features**:
- SQL injection prevention
- Shell command injection prevention
- Path traversal attack prevention
- LDAP injection prevention
- XSS protection (HTML escaping)
- SSRF prevention (URL validation)

**Creation**:
```go
sanitizer := config.NewSanitizer()
```

### 3. CommandValidator

Specialized validator for Docker command arguments and service names.

```go
type CommandValidator struct {
    serviceNamePattern *regexp.Regexp
    filePathPattern    *regexp.Regexp
    dangerousPatterns  []*regexp.Regexp
}
```

**Features**:
- Service name validation
- File path validation
- Command argument sanitization
- Dangerous pattern detection
- Null byte injection prevention

**Creation**:
```go
cmdValidator := config.NewCommandValidator()
```

## Validation Methods

### String Validation

**Required Field**:
```go
validator.ValidateRequired("job-name", jobName)
// Error if empty or whitespace-only
```

**Length Validation**:
```go
validator.ValidateMinLength("password", password, 8)
validator.ValidateMaxLength("username", username, 50)
```

**Format Validation**:
```go
// Email validation
validator.ValidateEmail("email-to", "admin@example.com")

// URL validation
validator.ValidateURL("webhook-url", "https://example.com/webhook")

// Cron expression validation
validator.ValidateCronExpression("schedule", "0 */6 * * *")
// Supports: standard cron, @daily, @hourly, @every 5m, etc.
```

**Enum Validation**:
```go
allowed := []string{"debug", "info", "warning", "error"}
validator.ValidateEnum("log-level", logLevel, allowed)
```

### Numeric Validation

**Range Validation**:
```go
// Port number validation
validator.ValidateRange("smtp-port", port, 1, 65535)
```

**Positive Value Validation**:
```go
validator.ValidatePositive("max-retries", retries)
```

### Path Validation

**Basic Path Check**:
```go
validator.ValidatePath("save-folder", "/var/log/ofelia")
// Checks for null bytes and invalid characters
```

## Sanitization Methods

### String Sanitization

**General String Sanitization**:
```go
sanitized, err := sanitizer.SanitizeString(input, 1024)
if err != nil {
    return err // Input too long or contains control characters
}
// Removes:
// - Null bytes (\x00)
// - Control characters (except \t, \n, \r)
// - Leading/trailing whitespace
```

**HTML Escaping**:
```go
safe := sanitizer.SanitizeHTML(userInput)
// Escapes <, >, &, ", ' to prevent XSS
```

### Command Validation

**Shell Command Validation**:
```go
err := sanitizer.ValidateCommand("/backup/script.sh --dry-run")
if err != nil {
    return err // Command contains dangerous characters or operations
}

// Blocks:
// - Shell operators: ; & | < > $ ` && || >> <<
// - Dangerous commands: rm -rf, dd if=, sudo, curl, wget
// - Fork bombs and format commands
```

**Command Argument Validation**:
```go
cmdValidator := config.NewCommandValidator()
args := []string{"--flag", "value", "/path/to/file"}
err := cmdValidator.ValidateCommandArgs(args)
// Checks for:
// - Dangerous patterns ($(...), backticks, pipes, etc.)
// - Null bytes
// - Excessive length (max 4096 chars per arg)
```

### Path Sanitization

**Path Traversal Prevention**:
```go
err := sanitizer.ValidatePath("/var/log/backup.log", "/var/log")
if err != nil {
    return err // Path traversal attempt or outside allowed directory
}

// Blocks:
// - ../ patterns
// - URL-encoded traversal (%2e%2e)
// - Dangerous extensions (.exe, .sh, .dll, etc.)
// - Paths outside allowed base path
```

**File Path Validation** (for Docker compose files):
```go
cmdValidator := config.NewCommandValidator()
err := cmdValidator.ValidateFilePath("/app/docker-compose.yml")
if err != nil {
    return err
}

// Blocks:
// - Sensitive directories (/etc/, /proc/, /sys/, /dev/)
// - Dangerous patterns
// - Invalid characters
// - Path length >4096 chars
```

### Environment Variable Validation

```go
err := sanitizer.ValidateEnvironmentVar("MY_VAR", "value123")
if err != nil {
    return err
}

// Validates:
// - Name format: [A-Za-z_][A-Za-z0-9_]*
// - Value length <4096 chars
// - No shell injection patterns in value
```

### Docker-Specific Validation

**Docker Image Name Validation**:
```go
err := sanitizer.ValidateDockerImage("nginx:1.21-alpine")
if err != nil {
    return err
}

// Validates format:
// [registry/]namespace/repository[:tag][@sha256:digest]
// Examples:
// - nginx:latest
// - docker.io/library/nginx:1.21
// - myregistry.com:5000/myapp/backend:v1.2.3
// - ubuntu@sha256:abcd...

// Blocks:
// - Invalid format
// - Suspicious patterns (.., //)
// - Length >255 chars
```

**Service Name Validation**:
```go
cmdValidator := config.NewCommandValidator()
err := cmdValidator.ValidateServiceName("web-backend")
if err != nil {
    return err
}

// Allows: alphanumeric, underscore, hyphen, dot
// Blocks: dangerous patterns, invalid characters
// Max length: 255 chars
```

### URL Validation

**SSRF Prevention**:
```go
err := sanitizer.ValidateURL("https://api.example.com/webhook")
if err != nil {
    return err
}

// Allows: http://, https:// only
// Blocks:
// - localhost, 127.0.0.1, 0.0.0.0
// - Internal networks (192.168.x.x, 10.x.x.x, 172.x.x.x)
// - .local domains
// - Direct IP addresses (configurable)
```

### Cron Expression Validation

**Comprehensive Cron Validation**:
```go
err := sanitizer.ValidateCronExpression("0 */6 * * *")
if err != nil {
    return err
}
```

**Supported Formats**:
1. **Standard Cron** (5 fields): `minute hour day month weekday`
   ```
   0 */6 * * *        # Every 6 hours
   30 2 * * 0         # 2:30 AM every Sunday
   0 0 1 * *          # Midnight on 1st of month
   ```

2. **Extended Cron** (6 fields): `second minute hour day month weekday`
   ```
   0 0 */6 * * *      # Every 6 hours with seconds
   ```

3. **Special Expressions**:
   ```
   @yearly   or @annually  # Once a year (0 0 1 1 *)
   @monthly                 # Once a month (0 0 1 * *)
   @weekly                  # Once a week (0 0 * * 0)
   @daily    or @midnight   # Once a day (0 0 * * *)
   @hourly                  # Once an hour (0 * * * *)
   ```

4. **@every Expressions**:
   ```
   @every 5m            # Every 5 minutes
   @every 1h            # Every hour
   @every 30s           # Every 30 seconds
   @every 2d            # Every 2 days
   ```

**Validation Rules**:
- **Ranges**: `1-5` (start must be < end)
- **Steps**: `*/5` or `0/10`
- **Lists**: `1,3,5,7`
- **Field limits**:
  - Minute: 0-59
  - Hour: 0-23
  - Day of month: 1-31
  - Month: 1-12
  - Day of week: 0-7 (0 and 7 are Sunday)
  - Second (if 6 fields): 0-59

### Email Validation

**Single Email**:
```go
validator.ValidateEmail("admin-email", "admin@example.com")
```

**Email List Validation**:
```go
err := sanitizer.ValidateEmailList("admin@example.com, ops@example.com")
if err != nil {
    return err
}
// Validates comma-separated list
// Format: local@domain.tld
```

## Integrated Configuration Validation

**ConfigValidator** (Validator2): Comprehensive struct validation using reflection.

```go
type Validator2 struct {
    config    interface{}
    sanitizer *Sanitizer
}
```

**Usage**:
```go
// Create configuration validator
configValidator := config.NewConfigValidator(myConfig)

// Perform validation
err := configValidator.Validate()
if err != nil {
    if valErrors, ok := err.(config.ValidationErrors); ok {
        for _, e := range valErrors {
            log.Printf("%s: %s (value: %v)", e.Field, e.Message, e.Value)
        }
    }
    return err
}
```

**Features**:
- Automatic field discovery via reflection
- Tag-based validation (`gcfg`, `mapstructure`, `default`)
- Nested struct validation
- Type-specific validators
- Security validation integration

**Struct Tag Support**:
```go
type JobConfig struct {
    Name     string `gcfg:"name" default:"my-job"`
    Schedule string `gcfg:"schedule"`
    Command  string `gcfg:"command"`
    Email    string `gcfg:"email-to" mapstructure:"email_to"`
}
```

## Security Attack Prevention

### SQL Injection Prevention

**Pattern Detection**:
```
union select, insert into, update set, delete from, drop table
create table, alter table, exec, execute, <script>, <iframe>
javascript:, eval(, setTimeout, setInterval, onload=, onerror=
```

**Usage**: Automatic via `SanitizeString()` for all string fields.

### Shell Injection Prevention

**Blocked Patterns**:
```
; & | < > $ ` \n \r
$( ${ && || >> << command substitution
```

**Blocked Commands**:
```
rm -rf, dd if=, mkfs, format, wget, curl, nc, telnet
chmod 777, chmod +x, sudo, su -, fork bombs
```

**Usage**:
```go
err := sanitizer.ValidateCommand(command)
```

### Path Traversal Prevention

**Blocked Patterns**:
```
../ ..\ ..%2F ..%2f %2e%2e
```

**Usage**:
```go
err := sanitizer.ValidatePath(path, allowedBasePath)
```

### LDAP Injection Prevention

**Blocked Characters**:
```
( ) * | & !
```

**Usage**: Automatic for fields containing LDAP-style data.

### XSS Prevention

**HTML Escaping**:
```go
safe := sanitizer.SanitizeHTML(userInput)
// <script> → &lt;script&gt;
```

### SSRF Prevention

**Blocked Targets**:
- Localhost: `localhost`, `127.0.0.1`, `0.0.0.0`
- Private networks: `192.168.x.x`, `10.x.x.x`, `172.16-31.x.x`
- Local domains: `*.local`
- Direct IPs (optional)

## Validation Examples

### Job Configuration Validation

```go
// Validate complete job configuration
func validateJobConfig(job *JobConfig) error {
    validator := config.NewValidator()
    sanitizer := config.NewSanitizer()

    // Required fields
    validator.ValidateRequired("name", job.Name)
    validator.ValidateRequired("schedule", job.Schedule)
    validator.ValidateRequired("command", job.Command)

    // Format validation
    validator.ValidateCronExpression("schedule", job.Schedule)

    // Security validation
    if err := sanitizer.ValidateCommand(job.Command); err != nil {
        validator.AddError("command", job.Command, err.Error())
    }

    if job.Image != "" {
        if err := sanitizer.ValidateDockerImage(job.Image); err != nil {
            validator.AddError("image", job.Image, err.Error())
        }
    }

    if job.EmailTo != "" {
        if err := sanitizer.ValidateEmailList(job.EmailTo); err != nil {
            validator.AddError("email-to", job.EmailTo, err.Error())
        }
    }

    if validator.HasErrors() {
        return validator.Errors()
    }

    return nil
}
```

### Environment Variable Validation

```go
func validateEnvironment(env map[string]string) error {
    sanitizer := config.NewSanitizer()

    for name, value := range env {
        if err := sanitizer.ValidateEnvironmentVar(name, value); err != nil {
            return fmt.Errorf("invalid environment variable %s: %w", name, err)
        }
    }

    return nil
}
```

### Docker Compose Job Validation

```go
func validateComposeJob(job *ComposeJob) error {
    validator := config.NewValidator()
    cmdValidator := config.NewCommandValidator()

    // Service name validation
    if err := cmdValidator.ValidateServiceName(job.Service); err != nil {
        validator.AddError("service", job.Service, err.Error())
    }

    // File path validation
    if err := cmdValidator.ValidateFilePath(job.File); err != nil {
        validator.AddError("file", job.File, err.Error())
    }

    return nil
}
```

## Error Handling

### ValidationError

Single validation error with context.

```go
type ValidationError struct {
    Field   string      // Field name (e.g., "email-to")
    Value   interface{} // Actual value that failed
    Message string      // Human-readable error message
}

// Example error:
// config validation error for field 'email-to': must be a valid email address (value: invalid-email)
```

### ValidationErrors

Multiple validation errors accumulated.

```go
type ValidationErrors []ValidationError

// Example output:
// config validation error for field 'schedule': invalid cron expression (value: invalid);
// config validation error for field 'email-to': must be a valid email address (value: bad@);
// config validation error for field 'smtp-port': must be between 1 and 65535 (value: 99999)
```

## Performance Considerations

- **Regex Compilation**: Patterns compiled once during sanitizer creation
- **Reflection Overhead**: ConfigValidator uses reflection - cache results for frequently validated configs
- **Validation Cost**: ~1-5ms for typical job configuration
- **String Operations**: O(n) for most validations where n = string length

## Best Practices

1. **Validate Early**: Validate configuration at load time, not runtime
2. **Accumulate Errors**: Use Validator to collect all errors, not fail-fast
3. **Layer Validation**: Combine format validation with security sanitization
4. **Sanitize User Input**: Always sanitize data from external sources
5. **Use Integrated Validator**: Prefer ConfigValidator for complete struct validation
6. **Check Defaults**: Consider default values when validating optional fields
7. **Whitelist Approach**: Prefer allowlists over denylists for security

## Testing

```go
import (
    "testing"
    "github.com/netresearch/ofelia/config"
)

func TestSanitizer(t *testing.T) {
    sanitizer := config.NewSanitizer()

    // Test shell injection detection
    err := sanitizer.ValidateCommand("rm -rf /")
    if err == nil {
        t.Error("Expected error for dangerous command")
    }

    // Test path traversal detection
    err = sanitizer.ValidatePath("../../etc/passwd", "/var/log")
    if err == nil {
        t.Error("Expected error for path traversal")
    }

    // Test valid cron expression
    err = sanitizer.ValidateCronExpression("0 */6 * * *")
    if err != nil {
        t.Errorf("Valid cron expression rejected: %v", err)
    }
}
```

## Integration Points

### CLI Integration

- **[Config Loader](../../cli/config.go)**: Uses validators during INI parsing
- **[Docker Labels](../../cli/docker-labels.go)**: Validates label-based configuration

### Core Integration

- **[Job Creation](../../core/job.go)**: Validates job parameters before execution
- **[Scheduler](../../core/scheduler.go)**: Validates cron expressions

### Middleware Integration

- **[Save Middleware](../middlewares.md)**: Uses path sanitization
- **[Mail Middleware](../middlewares.md)**: Uses email validation

## Related Documentation

- [CLI Package](./cli.md) - Configuration loading and parsing
- [Middlewares Package](./middlewares.md) - Middleware configuration validation
- [Web Package](./web.md) - API input validation
- [Security Considerations](../SECURITY.md) - Security best practices
- [PROJECT_INDEX](../PROJECT_INDEX.md) - Overall system architecture
