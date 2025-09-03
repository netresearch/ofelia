package logging

import (
	"encoding/json"
	"fmt"
	"io"
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
	Timestamp     time.Time              `json:"timestamp"`
	Level         string                 `json:"level"`
	Message       string                 `json:"message"`
	Fields        map[string]interface{} `json:"fields,omitempty"`
	Caller        string                 `json:"caller,omitempty"`
	StackTrace    string                 `json:"stack_trace,omitempty"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
}

// StructuredLogger provides structured logging capabilities
type StructuredLogger struct {
	mu            sync.RWMutex
	level         LogLevel
	output        io.Writer
	fields        map[string]interface{}
	correlationID string
	includeCaller bool
	jsonFormat    bool
}

// NewStructuredLogger creates a new structured logger
func NewStructuredLogger() *StructuredLogger {
	return &StructuredLogger{
		level:         InfoLevel,
		output:        os.Stdout,
		fields:        make(map[string]interface{}),
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
func (l *StructuredLogger) WithField(key string, value interface{}) *StructuredLogger {
	l.mu.RLock()
	defer l.mu.RUnlock()

	newFields := make(map[string]interface{})
	for k, v := range l.fields {
		newFields[k] = v
	}
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
func (l *StructuredLogger) WithFields(fields map[string]interface{}) *StructuredLogger {
	l.mu.RLock()
	defer l.mu.RUnlock()

	newFields := make(map[string]interface{})
	for k, v := range l.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}

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

	newLogger := *l
	newLogger.correlationID = id
	return &newLogger
}

// log writes a log entry
func (l *StructuredLogger) log(level LogLevel, message string, fields map[string]interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if level < l.level {
		return
	}

	entry := LogEntry{
		Timestamp:     time.Now(),
		Level:         level.String(),
		Message:       message,
		Fields:        make(map[string]interface{}),
		CorrelationID: l.correlationID,
	}

	// Merge logger fields
	for k, v := range l.fields {
		entry.Fields[k] = v
	}

	// Merge provided fields
	for k, v := range fields {
		entry.Fields[k] = v
	}

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
func (l *StructuredLogger) Debugf(format string, args ...interface{}) {
	l.log(DebugLevel, fmt.Sprintf(format, args...), nil)
}

// DebugWithFields logs a debug message with fields
func (l *StructuredLogger) DebugWithFields(message string, fields map[string]interface{}) {
	l.log(DebugLevel, message, fields)
}

// Info logs an info message
func (l *StructuredLogger) Info(message string) {
	l.log(InfoLevel, message, nil)
}

// Infof logs a formatted info message
func (l *StructuredLogger) Infof(format string, args ...interface{}) {
	l.log(InfoLevel, fmt.Sprintf(format, args...), nil)
}

// InfoWithFields logs an info message with fields
func (l *StructuredLogger) InfoWithFields(message string, fields map[string]interface{}) {
	l.log(InfoLevel, message, fields)
}

// Warn logs a warning message
func (l *StructuredLogger) Warn(message string) {
	l.log(WarnLevel, message, nil)
}

// Warnf logs a formatted warning message
func (l *StructuredLogger) Warnf(format string, args ...interface{}) {
	l.log(WarnLevel, fmt.Sprintf(format, args...), nil)
}

// WarnWithFields logs a warning message with fields
func (l *StructuredLogger) WarnWithFields(message string, fields map[string]interface{}) {
	l.log(WarnLevel, message, fields)
}

// Error logs an error message
func (l *StructuredLogger) Error(message string) {
	l.log(ErrorLevel, message, nil)
}

// Errorf logs a formatted error message
func (l *StructuredLogger) Errorf(format string, args ...interface{}) {
	l.log(ErrorLevel, fmt.Sprintf(format, args...), nil)
}

// ErrorWithFields logs an error message with fields
func (l *StructuredLogger) ErrorWithFields(message string, fields map[string]interface{}) {
	l.log(ErrorLevel, message, fields)
}

// Fatal logs a fatal message and exits
func (l *StructuredLogger) Fatal(message string) {
	l.log(FatalLevel, message, nil)
	os.Exit(1)
}

// Fatalf logs a formatted fatal message and exits
func (l *StructuredLogger) Fatalf(format string, args ...interface{}) {
	l.log(FatalLevel, fmt.Sprintf(format, args...), nil)
	os.Exit(1)
}

// FatalWithFields logs a fatal message with fields and exits
func (l *StructuredLogger) FatalWithFields(message string, fields map[string]interface{}) {
	l.log(FatalLevel, message, fields)
	os.Exit(1)
}

// JobLogger provides job-specific logging
type JobLogger struct {
	*StructuredLogger
	jobID   string
	jobName string
}

// NewJobLogger creates a logger for a specific job
func NewJobLogger(jobID, jobName string) *JobLogger {
	logger := NewStructuredLogger()
	return &JobLogger{
		StructuredLogger: logger.WithFields(map[string]interface{}{
			"job_id":   jobID,
			"job_name": jobName,
		}),
		jobID:   jobID,
		jobName: jobName,
	}
}

// LogStart logs job start
func (jl *JobLogger) LogStart() {
	jl.InfoWithFields("Job started", map[string]interface{}{
		"event": "job_start",
	})
}

// LogComplete logs job completion
func (jl *JobLogger) LogComplete(duration time.Duration, success bool) {
	fields := map[string]interface{}{
		"event":    "job_complete",
		"duration": duration.Seconds(),
		"success":  success,
	}

	if success {
		jl.InfoWithFields("Job completed successfully", fields)
	} else {
		jl.ErrorWithFields("Job failed", fields)
	}
}

// LogProgress logs job progress
func (jl *JobLogger) LogProgress(message string, percentComplete float64) {
	jl.InfoWithFields(message, map[string]interface{}{
		"event":    "job_progress",
		"progress": percentComplete,
	})
}

// Default logger instance
var DefaultLogger = NewStructuredLogger()

// Package-level convenience functions
func Debug(message string) { DefaultLogger.Debug(message) }
func Info(message string)  { DefaultLogger.Info(message) }
func Warn(message string)  { DefaultLogger.Warn(message) }
func Error(message string) { DefaultLogger.Error(message) }
func Fatal(message string) { DefaultLogger.Fatal(message) }
