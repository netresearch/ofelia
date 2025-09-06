# Core Package Documentation

## Overview
The `core` package contains the fundamental business logic for job scheduling, execution, and container orchestration in Ofelia.

## Key Components

### Job Types

#### BareJob
Base job structure with common fields and behavior.

```go
type BareJob struct {
    Name            string
    Schedule        string
    Command         string
    HistoryLimit    int
    ScheduleLock    sync.RWMutex
    // ... execution history tracking
}
```

**Key Methods:**
- `GetName()`: Returns job name
- `GetSchedule()`: Returns cron schedule
- `Hash()`: Generates configuration hash for change detection
- `SetLastRun()`: Records execution in history

#### RunJob
Executes commands in new Docker containers.

```go
type RunJob struct {
    BareJob
    Client    *docker.Client
    Image     string
    Network   string
    User      string
    Environment []string
    Volumes     []string
    // ... container configuration
}
```

**Key Features:**
- Creates ephemeral containers for job execution
- Supports image pulling policies
- Configurable networking and volumes
- Environment variable injection

#### ExecJob
Executes commands in existing containers.

```go
type ExecJob struct {
    BareJob
    Client    *docker.Client
    Container string
    User      string
    Environment []string
    Tty         bool
}
```

**Key Features:**
- Runs commands in already-running containers
- No container lifecycle management
- Useful for maintenance tasks

#### LocalJob
Executes commands directly on the host.

```go
type LocalJob struct {
    BareJob
    Dir         string
    Environment map[string]string
    User        string
}
```

**Security Considerations:**
- Runs with host privileges
- Environment variable inheritance
- Working directory configuration

#### ServiceJob
Runs jobs as Docker Swarm services.

```go
type ServiceJob struct {
    RunJob
    // Inherits RunJob configuration
    // Deployed as Swarm service
}
```

#### ComposeJob
Manages Docker Compose operations.

```go
type ComposeJob struct {
    LocalJob
    Project string
    Service string
    Timeout string
}
```

### Scheduler

Central scheduling engine using cron expressions.

```go
type Scheduler struct {
    jobs        map[string]JobI
    contexts    map[string]*Context
    logger      Logger
    location    *time.Location
    metrics     *MetricsCollector
    // ... scheduling state
}
```

**Key Methods:**
- `AddJob()`: Register new job
- `RemoveJob()`: Deregister job
- `Start()`: Begin scheduling
- `Stop()`: Graceful shutdown
- `RunJob()`: Manual job trigger

### Context

Execution context with middleware chain support.

```go
type Context struct {
    Scheduler   *Scheduler
    Logger      Logger
    Job         Job
    Execution   *Execution
    middlewares []Middleware
}
```

**Middleware Pattern:**
```go
func (c *Context) Next() error {
    middleware, exists := c.getNext()
    if !exists {
        return c.Job.Run(c)
    }
    return middleware.Run(c)
}
```

### Docker Integration

#### DockerClient
Wrapper for Docker API operations with metrics.

```go
type DockerClient struct {
    client          *docker.Client
    metricsRecorder MetricsRecorder
}
```

**Operations:**
- Container lifecycle management
- Image operations
- Network management
- Volume handling
- Swarm service deployment

#### ContainerMonitor
Tracks container lifecycle for dynamic job updates.

```go
type ContainerMonitor struct {
    client    *DockerClient
    scheduler *Scheduler
    metrics   MetricsRecorder
    stopCh    chan bool
}
```

**Features:**
- Event-based monitoring
- Polling fallback
- Label-based job discovery
- Automatic job registration/deregistration

### Resilience Patterns

#### RetryPolicy
Configurable retry behavior with exponential backoff.

```go
type RetryPolicy struct {
    MaxAttempts     int
    InitialDelay    time.Duration
    MaxDelay        time.Duration
    BackoffFactor   float64
    JitterFactor    float64
    RetryableErrors func(error) bool
}
```

#### CircuitBreaker
Prevents cascade failures.

```go
type CircuitBreaker struct {
    maxFailures     uint32
    resetTimeout    time.Duration
    state           CircuitBreakerState
    // ... failure tracking
}
```

**States:**
- `Closed`: Normal operation
- `Open`: Blocking requests
- `HalfOpen`: Testing recovery

#### RateLimiter
Token bucket rate limiting.

```go
type RateLimiter struct {
    rate     float64 // tokens per second
    capacity int     // max tokens
    tokens   float64
}
```

#### Bulkhead
Resource isolation pattern.

```go
type Bulkhead struct {
    maxConcurrent int
    semaphore     chan struct{}
}
```

### Error Handling

Custom error types for job execution scenarios:

```go
var (
    ErrSkippedExecution   = errors.New("skipped execution")
    ErrLocalImageNotFound = errors.New("local image not found")
    ErrUnexpected        = errors.New("unexpected error")
)
```

## Usage Examples

### Creating and Running a Job

```go
// Create a RunJob
job := &RunJob{
    BareJob: BareJob{
        Name:     "backup",
        Schedule: "@daily",
        Command:  "backup.sh",
    },
    Image: "alpine:latest",
    Client: dockerClient,
}

// Create scheduler
scheduler := NewScheduler(logger)
scheduler.AddJob(job)
scheduler.Start()
```

### Custom Middleware

```go
type LoggingMiddleware struct{}

func (m *LoggingMiddleware) Run(ctx *Context) error {
    ctx.Log("Starting job: " + ctx.Job.GetName())
    err := ctx.Next()
    if err != nil {
        ctx.Warn("Job failed: " + err.Error())
    }
    return err
}
```

### Resilient Execution

```go
executor := NewResilientJobExecutor(job)
executor.SetRetryPolicy(&RetryPolicy{
    MaxAttempts:  3,
    InitialDelay: 2 * time.Second,
})
executor.SetCircuitBreaker(
    NewCircuitBreaker("job", 5, 30*time.Second),
)
err := executor.Execute(ctx)
```

## Testing

The package includes comprehensive tests:

- Unit tests for all job types
- Integration tests for Docker operations
- Scheduler behavior tests
- Resilience pattern tests
- Container monitoring tests

## Performance Considerations

- Buffer pool for log streaming
- Concurrent job execution
- Event-based container monitoring
- Efficient cron expression parsing
- Metrics collection overhead minimization

## Security

- Input validation on all job parameters
- Container isolation
- Resource limits enforcement
- Secure environment variable handling
- Docker API authentication

---
*See also: [CLI Package](./cli.md) | [Web Package](./web.md) | [Project Index](../PROJECT_INDEX.md)*