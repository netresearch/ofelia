# Architecture and Design Patterns in Ofelia

## Core Architecture
- **Scheduler-based design**: Central scheduler manages all job execution
- **Job abstraction**: Interface-based job types (exec, run, local, compose, service)
- **Middleware pattern**: Extensible logging and notification system
- **Event-driven**: Docker event listening for dynamic container detection

## Key Components

### Core Package
- `Scheduler`: Main orchestrator using robfig/cron
- `Job` interface: Common abstraction for all job types
- `Context`: Execution context passed through middleware chain
- `Execution`: Captures job output and status
- `Logger` interface: Abstraction over logrus

### Job Types
- `ExecJob`: Execute in existing container
- `RunJob`: Create new container for execution
- `LocalJob`: Execute on host system
- `ComposeJob`: Docker Compose operations
- `ServiceJob`: Docker Swarm services

### Middleware System
- Chain of responsibility pattern
- Pre/post execution hooks
- Current middlewares: Slack, Email, Save, Overlap
- Extensible for custom middleware

## Concurrency Patterns
- `sync.RWMutex` for thread-safe access
- `sync.WaitGroup` for coordinating goroutines
- Atomic operations for job state
- Proper mutex protection for shared state

## Configuration
- INI file format with sections
- Docker labels for dynamic configuration
- Environment variable support
- Configuration validation before execution

## Web UI Architecture
- Embedded static files (no external dependencies)
- RESTful API endpoints
- Real-time job status updates
- Job history tracking

## Error Handling Strategy
- Error wrapping with context
- Package-level error variables
- Graceful degradation
- Comprehensive error logging

## Testing Strategy
- Unit tests for individual components
- Integration tests for Docker operations
- Test suites with check.v1 framework
- Mock Docker client for isolation