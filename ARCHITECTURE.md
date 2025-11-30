# Architecture Overview

This document provides a high-level overview of Ofelia's software architecture.

## System Overview

Ofelia is a Docker job scheduler that runs scheduled tasks in containers. It acts as a
modern replacement for cron in containerized environments.

```
┌─────────────────────────────────────────────────────────────────┐
│                         Ofelia Daemon                            │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │   Scheduler  │  │    Config    │  │   Notification       │  │
│  │   (cron)     │  │    Loader    │  │   System             │  │
│  └──────┬───────┘  └──────┬───────┘  └──────────┬───────────┘  │
│         │                 │                      │              │
│  ┌──────▼─────────────────▼──────────────────────▼───────────┐  │
│  │                     Job Runner                             │  │
│  │  ┌─────────────┐ ┌─────────────┐ ┌─────────────────────┐  │  │
│  │  │ job-exec    │ │ job-run     │ │ job-local           │  │  │
│  │  │ (exec in    │ │ (run new    │ │ (run on host)       │  │  │
│  │  │ container)  │ │ container)  │ │                     │  │  │
│  │  └─────────────┘ └─────────────┘ └─────────────────────┘  │  │
│  └────────────────────────┬──────────────────────────────────┘  │
│                           │                                      │
└───────────────────────────┼──────────────────────────────────────┘
                            │
                    ┌───────▼───────┐
                    │  Docker API   │
                    │  (socket/tcp) │
                    └───────┬───────┘
                            │
            ┌───────────────┼───────────────┐
            │               │               │
     ┌──────▼──────┐ ┌──────▼──────┐ ┌──────▼──────┐
     │ Container A │ │ Container B │ │ Container C │
     │ (job-exec)  │ │ (job-run)   │ │ (monitored) │
     └─────────────┘ └─────────────┘ └─────────────┘
```

## Core Components

### 1. Scheduler (`core/scheduler.go`)

The scheduler manages job timing using cron expressions:
- Parses cron expressions (standard and extended formats)
- Maintains job queue with next execution times
- Triggers job execution at scheduled times
- Supports multiple concurrent jobs

### 2. Configuration Loader (`cli/config.go`)

Loads job configurations from multiple sources:
- **INI file**: Traditional configuration file (`/etc/ofelia/config.ini`)
- **Docker labels**: Dynamic configuration from container labels
- **Environment variables**: Override settings via environment

Configuration is hot-reloaded when Docker events occur (container start/stop).

### 3. Job Types (`core/`)

| Job Type | Description | Use Case |
|----------|-------------|----------|
| `job-exec` | Execute command in running container | Run commands in existing services |
| `job-run` | Start new container for job | Isolated job execution |
| `job-local` | Execute command on host | System-level tasks |
| `job-service-run` | Run as Docker Swarm service | Swarm-native execution |
| `job-compose` | Run docker-compose jobs | Multi-container orchestration tasks |

### 4. Docker Integration (`core/docker_sdk_provider.go`, `core/docker_interface.go`)

Interfaces with Docker daemon via:
- Unix socket (`/var/run/docker.sock`)
- TCP connection (for remote Docker hosts)
- TLS authentication (optional)

Features:
- Container event monitoring for dynamic configuration
- Label-based job discovery
- Container lifecycle management

### 5. Notification System (`middlewares/`, e.g. `mail.go`, `slack.go`)

Sends job execution notifications via:
- **Email** (SMTP)
- **Slack** (webhook)
- **Gotify** (push notifications)
- **Custom webhooks** (HTTP POST)

Notifications include:
- Job start/completion status
- Execution output (stdout/stderr)
- Error details on failure

### 6. Logging (`logging/structured.go`, `core/logrus_logger.go`)

Structured logging with:
- Multiple log levels (debug, info, warn, error)
- JSON or text output formats
- Per-job log prefixing
- Output capture and storage

## Data Flow

### Job Execution Flow

```
1. Scheduler triggers job at scheduled time
         │
         ▼
2. Job Runner retrieves job configuration
         │
         ▼
3. Docker client executes job (exec/run/local)
         │
         ▼
4. Output captured and logged
         │
         ▼
5. Notifications sent (if configured)
         │
         ▼
6. Metrics updated (execution time, status)
```

### Configuration Reload Flow

```
1. Docker event received (container start/stop)
         │
         ▼
2. Config loader scans container labels
         │
         ▼
3. Jobs added/removed from scheduler
         │
         ▼
4. Scheduler continues with updated job list
```

## Security Considerations

- **Docker socket access**: Ofelia requires Docker socket access, which grants
  significant privileges. Run with minimal required permissions.
- **Input validation**: Job configurations are validated before execution
- **Output sanitization**: Sensitive data should not be logged
- **TLS support**: Secure connections to remote Docker hosts

See [SECURITY.md](SECURITY.md) for security policies and reporting vulnerabilities.

## Dependencies

Key external dependencies:
- `github.com/docker/docker` - Docker API client
- `github.com/netresearch/go-cron` - Cron expression parsing
- `github.com/fsouza/go-dockerclient` - Docker client wrapper
- `gopkg.in/ini.v1` - INI file parsing

Full dependency list in `go.mod`.

## Building and Testing

```bash
# Build
make build

# Run tests
make test

# Run with race detector
go test -race ./...

# Lint
make lint
```

## Directory Structure

```
ofelia/
├── ofelia.go               # Main entry point
├── cli/                    # Command-line interface implementation
├── core/                   # Core business logic
│   ├── scheduler.go        # Job scheduler
│   ├── docker_*.go         # Docker integration
│   └── *job.go             # Job type implementations
├── middlewares/            # Notification handlers (mail, slack, etc.)
├── logging/                # Structured logging
├── docs/                   # Documentation
├── .github/                # CI/CD workflows
└── docker-compose.yml      # Development environment
```

---

*For detailed API documentation, see the Go package documentation.*
