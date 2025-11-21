# Ofelia Architecture Diagrams

Comprehensive visual documentation of Ofelia's architecture, execution flows, and design patterns.

## Table of Contents
- [Job Execution Lifecycle](#job-execution-lifecycle)
- [Configuration Precedence](#configuration-precedence)
- [Docker Integration Architecture](#docker-integration-architecture)
- [Middleware Chain Execution](#middleware-chain-execution)
- [Resilience Patterns](#resilience-patterns)
- [Scheduler State Machine](#scheduler-state-machine)
- [Web UI & API Flow](#web-ui--api-flow)

---

## Job Execution Lifecycle

Complete lifecycle from schedule trigger to completion with all system interactions.

```mermaid
sequenceDiagram
    participant Cron as Cron Scheduler
    participant Sched as Scheduler
    participant Ctx as Context
    participant MW as Middleware Chain
    participant Job as Job (Run/Exec/Local)
    participant Docker as Docker Client
    participant Metrics as Metrics Collector
    participant Log as Logger

    Cron->>Sched: Trigger @ schedule time
    Sched->>Sched: Check no-overlap
    alt Job already running
        Sched-->>Cron: Skip execution
    end
    
    Sched->>Ctx: Create execution context
    Ctx->>Log: Log job start
    Ctx->>Metrics: Record job_started
    
    Ctx->>MW: Execute pre-middleware
    MW->>MW: Overlap check
    MW->>MW: Rate limit check
    MW->>MW: Sanitize inputs
    
    MW->>Job: Execute Run(ctx)
    
    alt RunJob (New Container)
        Job->>Docker: PullImage (if needed)
        Job->>Docker: CreateContainer
        Job->>Docker: StartContainer
        Job->>Docker: AttachLogs
        Docker-->>Job: stdout/stderr stream
        Job->>Docker: WaitContainer
        Job->>Docker: RemoveContainer (if delete=true)
    end
    
    alt ExecJob (Existing Container)
        Job->>Docker: CreateExec
        Job->>Docker: StartExec
        Docker-->>Job: stdout/stderr stream
        Job->>Docker: InspectExec
    end
    
    alt LocalJob (Host)
        Job->>Job: Execute os/exec.Command
        Job-->>Job: stdout/stderr capture
    end
    
    Job-->>MW: Return result + output
    MW->>MW: Execute post-middleware
    
    alt Email middleware
        MW->>MW: Send email notification
    end
    
    alt Slack middleware
        MW->>MW: Send slack message
    end
    
    alt Save middleware
        MW->>MW: Save execution report
    end
    
    MW-->>Ctx: Execution complete
    Ctx->>Metrics: Record job_duration
    Ctx->>Metrics: Record job_result
    Ctx->>Log: Log job completion
    Ctx->>Sched: Store execution history
    Sched-->>Cron: Ready for next trigger
```

---

## Configuration Precedence

How Ofelia merges configuration from multiple sources (5-layer system).

```mermaid
graph TB
    subgraph "Configuration Loading"
        A[1. Built-in Defaults] -->|Override| B[2. INI File]
        B -->|Override| C[3. Docker Labels]
        C -->|Override| D[4. CLI Flags]
        D -->|Override| E[5. Environment Variables]
    end
    
    subgraph "Built-in Defaults"
        A1[HistoryLimit: 10]
        A2[Delete: true]
        A3[LogLevel: INFO]
        A4[NoOverlap: false]
    end
    
    subgraph "INI File /etc/ofelia/config.ini"
        B1["[job-run 'backup']<br/>schedule = @daily<br/>image = postgres:15"]
        B2["[global]<br/>log-level = DEBUG<br/>enable-web = true"]
    end
    
    subgraph "Docker Labels"
        C1["ofelia.enabled=true"]
        C2["ofelia.job-exec.task.schedule=@hourly"]
        C3["ofelia.log-level=WARNING"]
    end
    
    subgraph "CLI Flags"
        D1["--config=/path/to/config.ini"]
        D2["--log-level=ERROR"]
        D3["--enable-web"]
    end
    
    subgraph "Environment Variables"
        E1["OFELIA_LOG_LEVEL=CRITICAL"]
        E2["OFELIA_ENABLE_WEB=true"]
        E3["OFELIA_WEB_ADDRESS=:9000"]
    end
    
    E --> F[Final Configuration]
    
    subgraph "Result log-level"
        F1["CRITICAL<br/>(from ENV)<br/>Highest Priority"]
    end
    
    style A fill:#e1f5ff
    style B fill:#d4edff
    style C fill:#b8daff
    style D fill:#9ecbff
    style E fill:#7eb6ff
    style F fill:#4a9eff,color:#fff
```

---

## Docker Integration Architecture

How Ofelia interacts with Docker daemon and manages containers.

```mermaid
graph TB
    subgraph "Ofelia Daemon"
        Sched[Scheduler]
        Monitor[Container Monitor]
        Jobs[Job Executors]
        Parser[Label Parser]
    end
    
    subgraph "Docker Daemon"
        API[Docker API]
        Events[Event Stream]
        Containers[Running Containers]
        Images[Image Registry]
        Networks[Networks]
        Volumes[Volumes]
    end
    
    subgraph "Configuration Sources"
        INI[ofelia.ini]
        Labels[Container Labels]
    end
    
    subgraph "Monitoring Mode"
        Poll[Polling<br/>default: 10s]
        EventMode[Event-based<br/>--docker-events]
    end
    
    INI --> Sched
    Labels --> Parser
    
    Monitor -->|Poll or Events| API
    API -->|Container List| Parser
    Parser -->|Job Definitions| Sched
    
    Poll -.->|--docker-poll-interval| Monitor
    EventMode -.->|--docker-events| Monitor
    
    Sched -->|Schedule Jobs| Jobs
    
    Jobs -->|RunJob| API
    API -->|Create/Start| Containers
    Jobs -->|ExecJob| API
    API -->|Exec Command| Containers
    
    Jobs -->|Pull Images| Images
    Jobs -->|Attach Networks| Networks
    Jobs -->|Mount Volumes| Volumes
    
    API -->|Event Stream| Events
    Events -->|Container Start/Stop| Monitor
    Monitor -->|Dynamic Updates| Sched
    
    style Sched fill:#4a9eff,color:#fff
    style Monitor fill:#ff9e4a,color:#fff
    style API fill:#9e4aff,color:#fff
```

---

## Middleware Chain Execution

Middleware execution order and context propagation.

```mermaid
graph LR
    subgraph "Pre-Execution Middleware"
        MW1[Overlap Check]
        MW2[Rate Limiter]
        MW3[Input Sanitizer]
        MW4[Circuit Breaker]
    end
    
    subgraph "Job Execution"
        Job[Job.Run ctx]
    end
    
    subgraph "Post-Execution Middleware"
        MW5[Output Capture]
        MW6[Metrics Recording]
        MW7[Email Notification]
        MW8[Slack Notification]
        MW9[Save Report]
    end
    
    Start([Context.Next]) --> MW1
    MW1 -->|Check running| MW2
    MW2 -->|Token bucket| MW3
    MW3 -->|Sanitize| MW4
    MW4 -->|Check state| Job
    
    Job -->|Result| MW5
    MW5 -->|Capture| MW6
    MW6 -->|Record| MW7
    MW7 -->|Email?| MW8
    MW8 -->|Slack?| MW9
    MW9 --> End([Return])
    
    MW1 -.->|Skip if running| End
    MW2 -.->|Rate limited| End
    MW4 -.->|Circuit open| End
    Job -.->|Error| MW5
    
    style Job fill:#4a9eff,color:#fff
    style Start fill:#4aff9e
    style End fill:#ff4a9e
```

---

## Resilience Patterns

Comprehensive resilience implementation with circuit breaker, retry, rate limiting.

```mermaid
stateDiagram-v2
    [*] --> ResilientJobExecutor
    
    ResilientJobExecutor --> RateLimiter: 1. Check rate
    RateLimiter --> CircuitBreaker: 2. Acquire token
    CircuitBreaker --> Bulkhead: 3. Check state
    Bulkhead --> RetryLoop: 4. Acquire semaphore
    
    state CircuitBreaker {
        [*] --> Closed
        Closed --> Open: Failures >= Threshold
        Open --> HalfOpen: Reset timeout
        HalfOpen --> Closed: Success
        HalfOpen --> Open: Failure
    }
    
    state RetryLoop {
        [*] --> Attempt
        Attempt --> Success: Job succeeds
        Attempt --> CheckRetry: Job fails
        CheckRetry --> Backoff: Attempts < Max
        Backoff --> Attempt: Wait + jitter
        CheckRetry --> Failed: Attempts >= Max
    }
    
    RateLimiter --> [*]: Rate exceeded
    CircuitBreaker --> [*]: Circuit open
    Bulkhead --> [*]: Max concurrency
    RetryLoop --> Metrics: Record result
    Metrics --> [*]
    
    note right of CircuitBreaker
        Prevents cascade failures
        Maxfailures: 5
        Reset timeout: 30s
    end note
    
    note right of RetryLoop
        Exponential backoff
        Jitter: 0.1
        Max attempts: 3
    end note
    
    note left of RateLimiter
        Token bucket algorithm
        Rate: tokens/second
        Burst capacity
    end note
```

---

## Scheduler State Machine

Scheduler lifecycle and state transitions.

```mermaid
stateDiagram-v2
    [*] --> Created: NewScheduler()
    Created --> Configured: LoadConfig()
    
    state Configured {
        [*] --> LoadingINI
        LoadingINI --> LoadingLabels
        LoadingLabels --> ValidatingJobs
        ValidatingJobs --> RegisteringJobs
        RegisteringJobs --> [*]
    }
    
    Configured --> Started: Start()
    
    state Started {
        [*] --> Scheduling
        
        state Scheduling {
            [*] --> Idle
            Idle --> Executing: Cron trigger
            Executing --> Idle: Job complete
            
            state Executing {
                [*] --> CheckOverlap
                CheckOverlap --> CreateContext
                CreateContext --> RunMiddleware
                RunMiddleware --> ExecuteJob
                ExecuteJob --> RecordMetrics
                RecordMetrics --> [*]
            }
        }
        
        Scheduling --> MonitoringDocker: Parallel
        
        state MonitoringDocker {
            [*] --> PollOrEvent
            PollOrEvent --> DetectChanges
            DetectChanges --> UpdateJobs: Hash changed
            UpdateJobs --> PollOrEvent
            DetectChanges --> PollOrEvent: No change
        }
        
        Scheduling --> WatchingINI: Parallel
        
        state WatchingINI {
            [*] --> WaitFileChange
            WaitFileChange --> ReloadConfig: File modified
            ReloadConfig --> WaitFileChange
        }
    }
    
    Started --> Stopping: Stop()
    
    state Stopping {
        [*] --> StoppingCron
        StoppingCron --> WaitingJobs: Wait running jobs
        WaitingJobs --> CleanupDocker: Timeout or complete
        CleanupDocker --> [*]
    }
    
    Stopping --> Stopped
    Stopped --> [*]
    
    note right of Scheduling
        Multiple jobs can
        execute concurrently
    end note
    
    note right of MonitoringDocker
        Configurable:
        - Polling (default 10s)
        - Event-based
        - Disabled (interval=0)
    end note
```

---

## Web UI & API Flow

HTTP request handling with JWT authentication and API endpoints.

```mermaid
sequenceDiagram
    participant Client
    participant Router
    participant AuthMW as Auth Middleware
    participant JWT as JWT Manager
    participant Handler
    participant Scheduler
    participant DB as Job Store
    
    rect rgb(240, 240, 255)
        Note over Client,JWT: Authentication Flow
        Client->>Router: POST /api/login
        Router->>Handler: Login handler
        Handler->>Handler: Verify credentials
        Handler->>JWT: GenerateToken(user)
        JWT-->>Handler: JWT token
        Handler-->>Client: 200 {token, expires}
    end
    
    rect rgb(255, 240, 240)
        Note over Client,DB: Protected API Request
        Client->>Router: GET /api/jobs<br/>Authorization: Bearer {token}
        Router->>AuthMW: Validate request
        AuthMW->>JWT: VerifyToken(token)
        JWT-->>AuthMW: Claims / Error
        
        alt Invalid token
            AuthMW-->>Client: 401 Unauthorized
        end
        
        AuthMW->>Handler: Request with claims
        Handler->>Scheduler: GetJobs()
        Scheduler->>DB: Query active jobs
        DB-->>Scheduler: Job list
        Scheduler-->>Handler: Job data
        Handler-->>Client: 200 {jobs: [...]}
    end
    
    rect rgb(240, 255, 240)
        Note over Client,Scheduler: Manual Job Execution
        Client->>Router: POST /api/jobs/backup/run
        Router->>AuthMW: Validate token
        AuthMW->>Handler: Authorized request
        Handler->>Scheduler: RunJob("backup")
        Scheduler->>Scheduler: Trigger immediate execution
        Scheduler-->>Handler: Execution started
        Handler-->>Client: 202 Accepted
    end
    
    rect rgb(255, 255, 240)
        Note over Client,DB: Job Management
        Client->>Router: PUT /api/jobs/backup
        Router->>AuthMW: Validate token
        AuthMW->>Handler: Authorized request
        Handler->>Scheduler: UpdateJob(config)
        Scheduler->>DB: Store new config
        Scheduler->>Scheduler: Reschedule job
        DB-->>Scheduler: Success
        Scheduler-->>Handler: Updated
        Handler-->>Client: 200 {job: {...}}
    end
```

---

## Component Interaction Map

High-level view of major component interactions.

```mermaid
graph TB
    subgraph "Entry Point"
        Main[ofelia.go<br/>CLI Entry]
    end
    
    subgraph "CLI Layer"
        Daemon[daemon.go<br/>Daemon Command]
        Validate[validate.go<br/>Config Validation]
        Config[config.go<br/>Config Manager]
    end
    
    subgraph "Core Engine"
        Scheduler[scheduler.go<br/>Job Scheduler]
        Context[context.go<br/>Execution Context]
        Jobs[Job Executors<br/>Run/Exec/Local/Service/Compose]
    end
    
    subgraph "Docker Integration"
        DockerClient[docker_client.go<br/>Docker API Wrapper]
        Monitor[container_monitor.go<br/>Container Lifecycle]
        Labels[docker-labels.go<br/>Label Parser]
    end
    
    subgraph "Web Layer"
        WebServer[server.go<br/>HTTP Server]
        JWTAuth[jwt_auth.go<br/>Authentication]
        HealthCheck[health.go<br/>Health Endpoints]
    end
    
    subgraph "Observability"
        Metrics[prometheus.go<br/>Metrics Collector]
        Logging[structured.go<br/>Structured Logger]
    end
    
    subgraph "Middlewares"
        Mail[mail.go<br/>Email Notifications]
        Slack[slack.go<br/>Slack Integration]
        Save[save.go<br/>Report Persistence]
        Overlap[overlap.go<br/>Concurrency Control]
    end
    
    subgraph "Security"
        Validator[validator.go<br/>Input Validation]
        Sanitizer[sanitizer.go<br/>Sanitization]
    end
    
    Main --> Daemon
    Main --> Validate
    
    Daemon --> Config
    Config --> Validator
    Config --> Sanitizer
    Config --> Scheduler
    
    Scheduler --> Jobs
    Scheduler --> Context
    Context --> Mail
    Context --> Slack
    Context --> Save
    Context --> Overlap
    
    Jobs --> DockerClient
    DockerClient --> Monitor
    Monitor --> Labels
    Labels --> Config
    
    Daemon --> WebServer
    WebServer --> JWTAuth
    WebServer --> HealthCheck
    WebServer --> Scheduler
    
    Jobs --> Metrics
    Jobs --> Logging
    Scheduler --> Metrics
    Context --> Logging
    
    style Main fill:#4a9eff,color:#fff
    style Scheduler fill:#ff9e4a,color:#fff
    style WebServer fill:#9e4aff,color:#fff
    style DockerClient fill:#4aff9e
    style Metrics fill:#ff4a9e,color:#fff
```

---

## Cross-References

- [Project Index](./PROJECT_INDEX.md)
- [Architecture Overview](./architecture.md)
- [Core Package Documentation](./packages/core.md)
- [CLI Package Documentation](./packages/cli.md)
- [Web Package Documentation](./packages/web.md)
- [Configuration Guide](./CONFIGURATION.md)
- [Security Documentation](./SECURITY.md)

---

*Generated: 2025-11-21 | Visual documentation for Ofelia architecture patterns and execution flows*
