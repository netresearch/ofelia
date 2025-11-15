# Logging Package

**Package**: `logging`
**Path**: `/logging/`
**Purpose**: Structured logging system with JSON/text output and metrics integration

## Overview

The logging package provides a thread-safe, structured logging system with support for contextual fields, correlation IDs, automatic caller tracking, and optional metrics integration. It supports both JSON and text output formats with configurable log levels.

## Key Types

### StructuredLogger

Main logger implementation with contextual field support.

```go
type StructuredLogger struct {
    mu            sync.RWMutex
    level         LogLevel
    output        io.Writer
    fields        map[string]interface{}
    correlationID string
    includeCaller bool
    jsonFormat    bool
}
```

**Creation**:
```go
logger := logging.NewStructuredLogger()
```

### LogLevel

Severity levels for filtering log messages.

```go
const (
    DebugLevel LogLevel = iota  // Detailed debugging information
    InfoLevel                   // General informational messages
    WarnLevel                   // Warning messages
    ErrorLevel                  // Error conditions
    FatalLevel                  // Fatal errors (does not exit)
)
```

### LogEntry

Structured representation of a log message.

```go
type LogEntry struct {
    Timestamp     time.Time              `json:"timestamp"`
    Level         string                 `json:"level"`
    Message       string                 `json:"message"`
    Fields        map[string]interface{} `json:"fields,omitempty"`
    Caller        string                 `json:"caller,omitempty"`
    StackTrace    string                 `json:"stackTrace,omitempty"`
    CorrelationID string                 `json:"correlationId,omitempty"`
}
```

### JobLogger

Specialized logger for job execution with automatic metrics integration.

```go
type JobLogger struct {
    *StructuredLogger
    jobID   string
    jobName string
    metrics MetricsCollector
}
```

## Core Features

### 1. Contextual Fields

Add persistent fields to logger instances.

```go
// Single field
logger = logger.WithField("user_id", "12345")

// Multiple fields
logger = logger.WithFields(map[string]interface{}{
    "service": "ofelia",
    "version": "1.0.0",
    "env":     "production",
})
```

### 2. Correlation IDs

Track requests across system boundaries.

```go
logger = logger.WithCorrelationID("req-abc-123")
logger.Info("Processing request")
// Output includes: "correlationId": "req-abc-123"
```

### 3. Automatic Caller Tracking

File, line, and function name automatically included (enabled by default).

```json
{
  "caller": "/app/core/scheduler.go:145 github.com/netresearch/ofelia/core.(*Scheduler).Start"
}
```

### 4. Stack Traces

Automatic stack traces for error and fatal levels.

```go
logger.Error("Database connection failed")
// Automatically includes full stack trace
```

### 5. Dual Output Formats

**JSON Format** (default):
```json
{
  "timestamp": "2025-01-15T10:30:45Z",
  "level": "INFO",
  "message": "Job started",
  "fields": {
    "job_name": "backup-db"
  }
}
```

**Text Format**:
```
2025-01-15T10:30:45Z [INFO] Job started {"job_name":"backup-db"}
```

## Logger Configuration

### Set Log Level

```go
logger.SetLevel(logging.DebugLevel) // Show all logs
logger.SetLevel(logging.InfoLevel)  // Hide debug logs
logger.SetLevel(logging.ErrorLevel) // Only errors and fatal
```

### Set Output Destination

```go
// Log to file
file, _ := os.Create("app.log")
logger.SetOutput(file)

// Log to multiple destinations
multiWriter := io.MultiWriter(os.Stdout, file)
logger.SetOutput(multiWriter)

// Log to buffer (for testing)
var buf bytes.Buffer
logger.SetOutput(&buf)
```

### Toggle JSON Format

```go
logger.SetJSONFormat(true)  // JSON output (default)
logger.SetJSONFormat(false) // Text output
```

## Logging Methods

### Basic Logging

```go
logger.Debug("Debugging information")
logger.Info("Normal operation")
logger.Warn("Warning condition")
logger.Error("Error occurred")
logger.Fatal("Fatal error") // Does NOT exit, caller handles
```

### Formatted Logging

```go
logger.Debugf("User %s logged in from %s", userID, ipAddr)
logger.Infof("Processed %d items in %v", count, duration)
logger.Errorf("Failed to connect to %s: %v", host, err)
```

### Logging with Fields

```go
logger.DebugWithFields("Cache lookup", map[string]interface{}{
    "key":   "user:123",
    "hit":   true,
    "ttl":   300,
})

logger.ErrorWithFields("Database error", map[string]interface{}{
    "operation": "INSERT",
    "table":     "jobs",
    "error":     err.Error(),
})
```

## Job-Specific Logging

### JobLogger Creation

```go
jobLogger := logging.NewJobLogger("job-001", "backup-db")

// Optional: Add metrics integration
jobLogger.SetMetricsCollector(metricsCollector)
```

### Job Lifecycle Logging

```go
// Log job start
jobLogger.LogStart()
// Output: {"job_id":"job-001", "job_name":"backup-db", "event":"job_start"}

// Log progress
jobLogger.LogProgress("Processing batch 3/10", 30.0)
// Output: {"event":"job_progress", "progress":30.0}

// Log errors
if err != nil {
    jobLogger.LogError(err, "Failed to backup table users")
}

// Log retries
jobLogger.LogRetry(attempt, maxAttempts, err)

// Log completion
jobLogger.LogComplete(duration, success)
// Output: {"event":"job_complete", "duration":45.2, "success":true}
```

### Automatic Metrics Integration

When metrics collector is configured, JobLogger automatically records:
- `jobs_started_total` counter
- `jobs_success_total` counter
- `jobs_failed_total` counter
- `job_errors_total` counter
- `job_retries_total` counter
- `jobs_running` gauge
- `job_progress_percent` gauge
- `job_duration_seconds` histogram

## Global Logger

### Using Default Logger

```go
import "github.com/netresearch/ofelia/logging"

// Package-level convenience functions
logging.Debug("Debugging info")
logging.Info("Information")
logging.Warn("Warning")
logging.Error("Error")
logging.Fatal("Fatal error")
```

### Replacing Default Logger

```go
// Configure custom default logger
customLogger := logging.NewStructuredLogger()
customLogger.SetLevel(logging.WarnLevel)
customLogger.SetOutput(logFile)

logging.DefaultLogger = customLogger
```

## Usage Examples

### Basic Application Logging

```go
import "github.com/netresearch/ofelia/logging"

logger := logging.NewStructuredLogger()
logger.SetLevel(logging.InfoLevel)

// Add application context
logger = logger.WithFields(map[string]interface{}{
    "app":     "ofelia",
    "version": "1.0.0",
})

logger.Info("Application started")
logger.Infof("Listening on port %d", port)
```

### Request Logging with Correlation

```go
func handleRequest(w http.ResponseWriter, r *http.Request) {
    correlationID := r.Header.Get("X-Correlation-ID")
    if correlationID == "" {
        correlationID = generateID()
    }

    reqLogger := logger.WithCorrelationID(correlationID)
    reqLogger = reqLogger.WithFields(map[string]interface{}{
        "method": r.Method,
        "path":   r.URL.Path,
        "ip":     r.RemoteAddr,
    })

    reqLogger.Info("Request started")
    // ... handle request ...
    reqLogger.Infof("Request completed in %v", duration)
}
```

### Job Execution Logging

```go
func executeJob(job Job, metrics MetricsCollector) error {
    jobLogger := logging.NewJobLogger(job.ID, job.Name)
    jobLogger.SetMetricsCollector(metrics)

    jobLogger.LogStart()
    start := time.Now()

    err := job.Run()
    duration := time.Since(start)

    if err != nil {
        jobLogger.LogError(err, "Job execution failed")
        jobLogger.LogComplete(duration, false)
        return err
    }

    jobLogger.LogComplete(duration, true)
    return nil
}
```

### Retry Logging

```go
func executeWithRetry(job Job, maxAttempts int) error {
    jobLogger := logging.NewJobLogger(job.ID, job.Name)

    for attempt := 1; attempt <= maxAttempts; attempt++ {
        err := job.Run()
        if err == nil {
            return nil
        }

        if attempt < maxAttempts {
            jobLogger.LogRetry(attempt, maxAttempts, err)
            time.Sleep(backoff(attempt))
        }
    }

    return fmt.Errorf("max retries exceeded")
}
```

### Error Logging with Context

```go
func connectDatabase(config DBConfig) error {
    logger := logging.NewStructuredLogger()
    logger = logger.WithFields(map[string]interface{}{
        "host": config.Host,
        "port": config.Port,
        "db":   config.Database,
    })

    logger.Info("Connecting to database")

    conn, err := sql.Open("postgres", config.DSN())
    if err != nil {
        logger.ErrorWithFields("Database connection failed", map[string]interface{}{
            "error": err.Error(),
            "dsn":   config.DSN(),
        })
        return err
    }

    logger.Info("Database connection established")
    return nil
}
```

## Integration Points

### Core Integration
- **[Scheduler](../../core/scheduler.go)**: Job execution logging
- **[ResilientJob](../../core/resilience.go)**: Retry and error logging
- **[Context](../../core/common.go)**: Contextual job logging

### Metrics Integration
- **[Prometheus](./metrics.md)**: Automatic metrics from JobLogger
- **MetricsCollector Interface**: Bridge between logging and metrics

### Web Integration
- **[Server](../../web/server.go)**: Request/response logging
- **[Middleware](../../web/middleware.go)**: HTTP request correlation

## Thread Safety

All logging operations are thread-safe using `sync.RWMutex`:
- Field updates and format changes use `Lock()`
- Logging operations use `RLock()`
- Concurrent logging from multiple goroutines is safe

## Performance Considerations

- **Field Copying**: `WithField()` creates new logger instances (immutable pattern)
- **Caller Tracking**: Uses `runtime.Caller()` - disable for hot paths
- **Stack Traces**: Only captured for Error/Fatal levels
- **JSON Encoding**: Slightly slower than text format
- **Lock Contention**: Read-heavy design minimizes write lock duration

## Testing

```go
import (
    "bytes"
    "testing"
    "github.com/netresearch/ofelia/logging"
)

func TestLogging(t *testing.T) {
    var buf bytes.Buffer
    logger := logging.NewStructuredLogger()
    logger.SetOutput(&buf)
    logger.SetJSONFormat(true)

    logger.Info("Test message")

    output := buf.String()
    if !strings.Contains(output, "Test message") {
        t.Errorf("Expected log message in output")
    }
}
```

## Best Practices

1. **Use Structured Fields**: Prefer `InfoWithFields()` over string concatenation
2. **Add Context Early**: Use `WithFields()` to set persistent context
3. **Use Correlation IDs**: Track requests across service boundaries
4. **Log at Appropriate Levels**: Debug for development, Info for production
5. **Avoid Logging Secrets**: Never log passwords, tokens, or sensitive data
6. **Use JobLogger for Jobs**: Automatic metrics and standardized events
7. **Test Log Output**: Verify critical logs in tests using buffer output

## Log Level Guidelines

| Level | When to Use | Examples |
|-------|-------------|----------|
| **Debug** | Development debugging | "Cache miss for key X", "Entering function Y" |
| **Info** | Normal operations | "Job started", "Request completed", "Service ready" |
| **Warn** | Recoverable issues | "Retry attempt 2/3", "Deprecated API used" |
| **Error** | Error conditions | "Job failed", "Database error", "API timeout" |
| **Fatal** | Critical failures | "Config missing", "Cannot bind port" (caller exits) |

## Related Documentation

- [Metrics Package](./metrics.md) - Metrics integration
- [Core Package](./core.md) - Job execution logging
- [Web Package](./web.md) - HTTP request logging
- [PROJECT_INDEX](../PROJECT_INDEX.md) - Overall system architecture
