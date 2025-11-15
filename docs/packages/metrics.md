# Metrics Package

**Package**: `metrics`
**Path**: `/metrics/`
**Purpose**: Prometheus-compatible metrics collection and HTTP endpoint exposure

## Overview

The metrics package provides a thread-safe, Prometheus-compatible metrics collection system for monitoring Ofelia's job execution, Docker operations, and HTTP API performance. It implements counters, gauges, and histograms with automatic initialization of default metrics.

## Key Types

### Collector

Thread-safe metrics collector that manages all application metrics.

```go
type Collector struct {
    mu      sync.RWMutex
    metrics map[string]*Metric
}
```

**Methods**:
- `NewCollector() *Collector` - Creates new metrics collector
- `InitDefaultMetrics()` - Initializes standard Ofelia metrics
- `Export() string` - Exports metrics in Prometheus text format
- `Handler() http.HandlerFunc` - Returns HTTP handler for `/metrics` endpoint

### Metric Types

#### Counter
Monotonically increasing value (e.g., total jobs executed).

```go
RegisterCounter(name, help string)
IncrementCounter(name string, value float64)
```

#### Gauge
Value that can go up or down (e.g., currently running jobs).

```go
RegisterGauge(name, help string)
SetGauge(name string, value float64)
```

#### Histogram
Distribution of values in configurable buckets (e.g., job duration).

```go
RegisterHistogram(name, help string, buckets []float64)
ObserveHistogram(name string, value float64)
```

### JobMetrics

Tracks job execution lifecycle metrics with automatic duration calculation.

```go
type JobMetrics struct {
    collector *Collector
    startTime map[string]time.Time
    mu        sync.Mutex
}
```

**Usage**:
```go
jm := NewJobMetrics(collector)
jm.JobStarted("backup-001")
// ... job execution ...
jm.JobCompleted("backup-001", success)
```

## Default Metrics

The collector automatically initializes these Prometheus metrics:

### Job Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `ofelia_jobs_total` | Counter | Total jobs executed |
| `ofelia_jobs_failed_total` | Counter | Total failed jobs |
| `ofelia_jobs_running` | Gauge | Currently running jobs |
| `ofelia_job_duration_seconds` | Histogram | Job execution duration |

### Docker Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `ofelia_docker_operations_total` | Counter | Docker API operations |
| `ofelia_docker_errors_total` | Counter | Docker API errors |

### Container Monitoring Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `ofelia_container_monitor_events_total` | Counter | Container events received |
| `ofelia_container_monitor_fallbacks_total` | Counter | Fallbacks to polling |
| `ofelia_container_monitor_method` | Gauge | Monitoring method (1=events, 0=polling) |
| `ofelia_container_wait_duration_seconds` | Histogram | Container wait duration |

### Retry Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `ofelia_job_retries_total` | Counter | Total retry attempts |
| `ofelia_job_retry_success_total` | Counter | Successful retries |
| `ofelia_job_retry_failed_total` | Counter | Failed retries |
| `ofelia_job_retry_delay_seconds` | Histogram | Retry delay distribution |

### HTTP Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `ofelia_http_requests_total` | Counter | Total HTTP requests |
| `ofelia_http_request_duration_seconds` | Histogram | HTTP request duration |

### System Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `ofelia_up` | Gauge | Service status (1=up, 0=down) |
| `ofelia_restarts_total` | Counter | Service restarts |

## Usage Examples

### Basic Setup

```go
import "github.com/netresearch/ofelia/metrics"

// Create collector
collector := metrics.NewCollector()

// Initialize default metrics
collector.InitDefaultMetrics()

// Expose metrics endpoint
http.Handle("/metrics", collector.Handler())
```

### Custom Metrics

```go
// Register custom counter
collector.RegisterCounter(
    "ofelia_custom_events_total",
    "Total custom events processed",
)

// Increment counter
collector.IncrementCounter("ofelia_custom_events_total", 1)

// Register custom histogram with buckets
collector.RegisterHistogram(
    "ofelia_custom_duration_seconds",
    "Custom operation duration",
    []float64{0.1, 0.5, 1, 5, 10},
)

// Observe value
collector.ObserveHistogram("ofelia_custom_duration_seconds", 2.5)
```

### Job Metrics Tracking

```go
// Create job metrics tracker
jobMetrics := metrics.NewJobMetrics(collector)

// Track job execution
jobID := "backup-db-001"
jobMetrics.JobStarted(jobID)

// Execute job...
success := executeJob()

// Record completion (automatically calculates duration)
jobMetrics.JobCompleted(jobID, success)
```

### HTTP Middleware

```go
// Wrap HTTP handlers with metrics
handler := metrics.HTTPMetrics(collector)(yourHandler)
http.Handle("/api/jobs", handler)
```

### Docker Operations

```go
// Record Docker operation
collector.RecordDockerOperation("container.create")

// Record Docker error
collector.RecordDockerError("container.start")
```

### Container Monitoring

```go
// Record container event
collector.RecordContainerEvent()

// Record fallback to polling
collector.RecordContainerMonitorFallback()

// Set monitoring method
collector.RecordContainerMonitorMethod(usingEvents)

// Record wait duration
collector.RecordContainerWaitDuration(seconds)
```

### Retry Tracking

```go
// Record retry attempt
collector.RecordJobRetry(
    jobName,
    attemptNumber,
    success,
)
```

## Integration Points

### Core Integration
- **[Scheduler](../../core/scheduler.go)**: Job execution metrics
- **[ResilientJob](../../core/resilience.go)**: Retry and circuit breaker metrics
- **[Docker Client](../../core/docker_client.go)**: Docker operation metrics
- **[Container Monitor](../../core/container_monitor.go)**: Container event metrics

### Web Integration
- **[Server](../../web/server.go)**: HTTP metrics and `/metrics` endpoint
- **[Middleware](../../web/middleware.go)**: Request duration tracking

## Prometheus Configuration

To scrape metrics from Ofelia:

```yaml
scrape_configs:
  - job_name: 'ofelia'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
    scrape_interval: 15s
```

## Grafana Dashboard Example

```json
{
  "panels": [
    {
      "title": "Jobs Executed",
      "targets": [
        {
          "expr": "rate(ofelia_jobs_total[5m])"
        }
      ]
    },
    {
      "title": "Job Duration (p95)",
      "targets": [
        {
          "expr": "histogram_quantile(0.95, ofelia_job_duration_seconds_bucket)"
        }
      ]
    },
    {
      "title": "Currently Running Jobs",
      "targets": [
        {
          "expr": "ofelia_jobs_running"
        }
      ]
    }
  ]
}
```

## Thread Safety

All metric operations are thread-safe using `sync.RWMutex`:
- Read operations use `RLock()`
- Write operations use `Lock()`
- Concurrent metric updates are safely handled

## Performance Considerations

- **Lock Contention**: Read-heavy workloads benefit from `RWMutex`
- **Memory Usage**: ~100 bytes per metric + histogram buckets
- **Export Performance**: O(n) where n = number of metrics
- **HTTP Handler**: Acquires read lock for full export duration

## Testing

```bash
# Run metrics tests
go test ./metrics -v

# Check metric export format
curl http://localhost:8080/metrics
```

## Related Documentation

- [Core Package](./core.md) - Job execution integration
- [Web Package](./web.md) - HTTP endpoint integration
- [API Documentation](../API.md) - Metrics endpoint details
- [PROJECT_INDEX](../PROJECT_INDEX.md) - Overall system architecture
