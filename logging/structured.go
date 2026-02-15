package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"runtime"
	"sync"
	"time"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	DebugLevel LogLevel = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

func (l LogLevel) String() string {
	switch l {
	case DebugLevel:
		return "DEBUG"
	case InfoLevel:
		return "INFO"
	case WarnLevel:
		return "WARN"
	case ErrorLevel:
		return "ERROR"
	case FatalLevel:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp     time.Time      `json:"timestamp"`
	Level         string         `json:"level"`
	Message       string         `json:"message"`
	Fields        map[string]any `json:"fields,omitempty"`
	Caller        string         `json:"caller,omitempty"`
	StackTrace    string         `json:"stackTrace,omitempty"`
	CorrelationID string         `json:"correlationId,omitempty"`
}

// StructuredLogger provides structured logging capabilities
type StructuredLogger struct {
	mu            sync.RWMutex
	level         LogLevel
	output        io.Writer
	fields        map[string]any
	correlationID string
	includeCaller bool
	jsonFormat    bool
}

// NewStructuredLogger creates a new structured logger
func NewStructuredLogger() *StructuredLogger {
	return &StructuredLogger{
		level:         InfoLevel,
		output:        os.Stdout,
		fields:        make(map[string]any),
		includeCaller: true,
		jsonFormat:    true,
	}
}

// SetLevel sets the minimum log level
func (l *StructuredLogger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetOutput sets the output writer
func (l *StructuredLogger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.output = w
}

// SetJSONFormat enables/disables JSON formatting
func (l *StructuredLogger) SetJSONFormat(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.jsonFormat = enabled
}

// WithField creates a new logger with an additional field
func (l *StructuredLogger) WithField(key string, value any) *StructuredLogger {
	l.mu.RLock()
	defer l.mu.RUnlock()

	newFields := make(map[string]any)
	maps.Copy(newFields, l.fields)
	newFields[key] = value

	return &StructuredLogger{
		level:         l.level,
		output:        l.output,
		fields:        newFields,
		correlationID: l.correlationID,
		includeCaller: l.includeCaller,
		jsonFormat:    l.jsonFormat,
	}
}

// WithFields creates a new logger with additional fields
func (l *StructuredLogger) WithFields(fields map[string]any) *StructuredLogger {
	l.mu.RLock()
	defer l.mu.RUnlock()

	newFields := make(map[string]any)
	maps.Copy(newFields, l.fields)
	maps.Copy(newFields, fields)

	return &StructuredLogger{
		level:         l.level,
		output:        l.output,
		fields:        newFields,
		correlationID: l.correlationID,
		includeCaller: l.includeCaller,
		jsonFormat:    l.jsonFormat,
	}
}

// WithCorrelationID sets a correlation ID for tracing
func (l *StructuredLogger) WithCorrelationID(id string) *StructuredLogger {
	l.mu.RLock()
	defer l.mu.RUnlock()

	newLogger := &StructuredLogger{
		level:         l.level,
		output:        l.output,
		fields:        l.fields,
		correlationID: id,
		includeCaller: l.includeCaller,
		jsonFormat:    l.jsonFormat,
	}
	return newLogger
}

// log writes a log entry
func (l *StructuredLogger) log(level LogLevel, message string, fields map[string]any) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if level < l.level {
		return
	}

	entry := LogEntry{
		Timestamp:     time.Now(),
		Level:         level.String(),
		Message:       message,
		Fields:        make(map[string]any),
		CorrelationID: l.correlationID,
	}

	// Merge logger fields
	maps.Copy(entry.Fields, l.fields)

	// Merge provided fields
	maps.Copy(entry.Fields, fields)

	// Add caller information
	if l.includeCaller {
		if pc, file, line, ok := runtime.Caller(2); ok {
			f := runtime.FuncForPC(pc)
			entry.Caller = fmt.Sprintf("%s:%d %s", file, line, f.Name())
		}
	}

	// Add stack trace for errors
	if level >= ErrorLevel {
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		entry.StackTrace = string(buf[:n])
	}

	// Format and write
	if l.jsonFormat {
		encoder := json.NewEncoder(l.output)
		_ = encoder.Encode(entry)
	} else {
		fmt.Fprintf(l.output, "%s [%s] %s",
			entry.Timestamp.Format(time.RFC3339),
			entry.Level,
			entry.Message)

		if len(entry.Fields) > 0 {
			fmt.Fprintf(l.output, " %v", entry.Fields)
		}

		if entry.CorrelationID != "" {
			fmt.Fprintf(l.output, " [%s]", entry.CorrelationID)
		}

		fmt.Fprintln(l.output)
	}
}

// Debug logs a debug message
func (l *StructuredLogger) Debug(message string) {
	l.log(DebugLevel, message, nil)
}

// Debugf logs a formatted debug message
func (l *StructuredLogger) Debugf(format string, args ...any) {
	l.log(DebugLevel, fmt.Sprintf(format, args...), nil)
}

// DebugWithFields logs a debug message with fields
func (l *StructuredLogger) DebugWithFields(message string, fields map[string]any) {
	l.log(DebugLevel, message, fields)
}

// Info logs an info message
func (l *StructuredLogger) Info(message string) {
	l.log(InfoLevel, message, nil)
}

// Infof logs a formatted info message
func (l *StructuredLogger) Infof(format string, args ...any) {
	l.log(InfoLevel, fmt.Sprintf(format, args...), nil)
}

// InfoWithFields logs an info message with fields
func (l *StructuredLogger) InfoWithFields(message string, fields map[string]any) {
	l.log(InfoLevel, message, fields)
}

// Warn logs a warning message
func (l *StructuredLogger) Warn(message string) {
	l.log(WarnLevel, message, nil)
}

// Warnf logs a formatted warning message
func (l *StructuredLogger) Warnf(format string, args ...any) {
	l.log(WarnLevel, fmt.Sprintf(format, args...), nil)
}

// WarnWithFields logs a warning message with fields
func (l *StructuredLogger) WarnWithFields(message string, fields map[string]any) {
	l.log(WarnLevel, message, fields)
}

// Error logs an error message
func (l *StructuredLogger) Error(message string) {
	l.log(ErrorLevel, message, nil)
}

// Errorf logs a formatted error message
func (l *StructuredLogger) Errorf(format string, args ...any) {
	l.log(ErrorLevel, fmt.Sprintf(format, args...), nil)
}

// ErrorWithFields logs an error message with fields
func (l *StructuredLogger) ErrorWithFields(message string, fields map[string]any) {
	l.log(ErrorLevel, message, fields)
}

// Fatal logs a fatal message. Note: Does not exit automatically.
// Caller should handle the fatal condition appropriately.
func (l *StructuredLogger) Fatal(message string) {
	l.log(FatalLevel, message, nil)
	// Removed os.Exit(1) - let caller decide how to handle fatal errors
}

// Fatalf logs a formatted fatal message. Note: Does not exit automatically.
// Caller should handle the fatal condition appropriately.
func (l *StructuredLogger) Fatalf(format string, args ...any) {
	l.log(FatalLevel, fmt.Sprintf(format, args...), nil)
	// Removed os.Exit(1) - let caller decide how to handle fatal errors
}

// FatalWithFields logs a fatal message with fields. Note: Does not exit automatically.
// Caller should handle the fatal condition appropriately.
func (l *StructuredLogger) FatalWithFields(message string, fields map[string]any) {
	l.log(FatalLevel, message, fields)
	// Removed os.Exit(1) - let caller decide how to handle fatal errors
}

// JobLogger provides job-specific logging
type JobLogger struct {
	*StructuredLogger
	jobID   string
	jobName string
	metrics MetricsCollector
}

// NewJobLogger creates a logger for a specific job
func NewJobLogger(jobID, jobName string) *JobLogger {
	logger := NewStructuredLogger()
	return &JobLogger{
		StructuredLogger: logger.WithFields(map[string]any{
			"job_id":   jobID,
			"job_name": jobName,
		}),
		jobID:   jobID,
		jobName: jobName,
	}
}

// SetMetricsCollector sets the metrics collector for the job logger
func (jl *JobLogger) SetMetricsCollector(metrics MetricsCollector) {
	jl.metrics = metrics
}

// LogStart logs job start
func (jl *JobLogger) LogStart() {
	jl.InfoWithFields("Job started", map[string]any{
		"event": "job_start",
	})

	// Update metrics if available
	if jl.metrics != nil {
		jl.metrics.IncrementCounter("jobs_started_total", 1)
		jl.metrics.SetGauge("jobs_running", 1)
	}
}

// LogComplete logs job completion
func (jl *JobLogger) LogComplete(duration time.Duration, success bool) {
	fields := map[string]any{
		"event":    "job_complete",
		"duration": duration.Seconds(),
		"success":  success,
	}

	if success {
		jl.InfoWithFields("Job completed successfully", fields)
		if jl.metrics != nil {
			jl.metrics.IncrementCounter("jobs_success_total", 1)
		}
	} else {
		jl.ErrorWithFields("Job failed", fields)
		if jl.metrics != nil {
			jl.metrics.IncrementCounter("jobs_failed_total", 1)
		}
	}

	// Record duration in metrics
	if jl.metrics != nil {
		jl.metrics.ObserveHistogram("job_duration_seconds", duration.Seconds())
		jl.metrics.SetGauge("jobs_running", -1)
	}
}

// LogProgress logs job progress
func (jl *JobLogger) LogProgress(message string, percentComplete float64) {
	jl.InfoWithFields(message, map[string]any{
		"event":    "job_progress",
		"progress": percentComplete,
	})

	// Update progress gauge
	if jl.metrics != nil {
		jl.metrics.SetGauge("job_progress_percent", percentComplete)
	}
}

// LogError logs an error with context
func (jl *JobLogger) LogError(err error, context string) {
	jl.ErrorWithFields("Job error occurred", map[string]any{
		"event":   "job_error",
		"error":   err.Error(),
		"context": context,
	})

	if jl.metrics != nil {
		jl.metrics.IncrementCounter("job_errors_total", 1)
	}
}

// LogRetry logs a retry attempt
func (jl *JobLogger) LogRetry(attempt int, maxAttempts int, err error) {
	jl.WarnWithFields("Retrying job execution", map[string]any{
		"event":        "job_retry",
		"attempt":      attempt,
		"max_attempts": maxAttempts,
		"error":        err.Error(),
	})

	if jl.metrics != nil {
		jl.metrics.IncrementCounter("job_retries_total", 1)
	}
}

// Default logger instance
var DefaultLogger = NewStructuredLogger()

// MetricsCollector interface for logging metrics integration
type MetricsCollector interface {
	IncrementCounter(name string, value float64)
	SetGauge(name string, value float64)
	ObserveHistogram(name string, value float64)
}

// Package-level convenience functions
func Debug(message string) { DefaultLogger.Debug(message) }
func Info(message string)  { DefaultLogger.Info(message) }
func Warn(message string)  { DefaultLogger.Warn(message) }
func Error(message string) { DefaultLogger.Error(message) }
func Fatal(message string) { DefaultLogger.Fatal(message) }
