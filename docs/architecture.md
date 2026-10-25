# Architecture Overview

Ofelia is built around a pluggable scheduler that manages different job types and middleware.

## Scheduler

The scheduler wraps [`netresearch/go-cron`](https://github.com/netresearch/go-cron)
(a maintained fork of `robfig/cron` with a DAG engine, pause/resume, and
`@triggered` schedules) and keeps track of all registered jobs. It runs them
according to their cron expressions and exposes methods to start or stop
execution. Each job is wrapped so execution metadata and middleware can be
handled.

Jobs are registered by name for O(1) lookup, update and removal.
`AddJobWithTags` builds `cron.JobOption`s before handing the job to go-cron:

```go
opts := []cron.JobOption{cron.WithName(j.GetName())}
if len(tags) > 0 {
    opts = append(opts, cron.WithTags(tags...))
}
if j.ShouldRunOnStartup() {
    opts = append(opts, cron.WithRunImmediately())
}
w := &jobWrapper{s: s, j: j}
id, err := s.cron.AddJob(j.GetSchedule(), w, opts...)
```

Removal cancels a run in progress and drops the job from scheduler state at
once; update replaces the entry in place by name and cancels the old job's run
the same way. Neither waits for the canceled run to stop, which can take a
while, for example to remove a container:

```go
s.gate.retire(j.GetName())
s.cron.RemoveByName(j.GetName())
```

Until that run has stopped, `IsRemovedJobRunning` reports it, and a job
registered under the same name skips its fires with a warning, so the old and
the new job never run at the same time. A skip, not a wait: a waiting fire would
hold a slot of the global concurrency limit for as long as the old run takes.
The bookkeeping is `runGate` in `core/run_gate.go`.

See `core/scheduler.go` (`AddJobWithTags`, `RemoveJob`, `UpdateJob`, `DisableJob`/`EnableJob`).

## Job types

Every job type implements the `Job` interface (`core/common.go`) and embeds
`BareJob` for the shared bookkeeping (name, schedule, execution history, retry
config, middleware chain), so a new job type only has to add its own config
fields and a `Run` method:

```go
type Job interface {
    GetName() string
    GetSchedule() string
    GetCommand() string
    Run(*Context) error
    Middlewares() []Middleware
    Use(...Middleware)
    // ... history, retry and cron-id accessors
}

type ExecJob struct {
    BareJob   `mapstructure:",squash"`
    Provider  DockerProvider
    Container string `hash:"true"`
    // ... exec-specific fields (User, Environment, WorkingDir, ...)
}
```

Jobs can be executed in several ways:

- **`exec`** (`core/execjob.go`) – run a command inside an existing container
  similar to `docker exec`.
- **`run`** (`core/runjob.go`) – start a new container for every execution.
- **`local`** (`core/localjob.go`) – execute a command on the host where
  Ofelia runs.
- **`service-run`** (`core/runservice.go`) – run a one‑off service in a
  Docker swarm.

## Middleware

Middleware is a chain-of-responsibility, not a wrapper function: each
`Middleware.Run(*Context)` must call `ctx.Next()` to continue to the next
middleware and, once the chain is exhausted, to the job itself.

Short-circuiting (e.g. the overlap middleware skipping an already-running job)
is done by calling `ctx.Stop(err)`, not by omitting `ctx.Next()`: the middleware
still calls `ctx.Next()`, and `doNext()` then stops advancing to any subsequent
middleware whose `ContinueOnStop()` is false (`core/common.go`).

```go
type Middleware interface {
    Run(*Context) error   // MUST call ctx.Next() to continue the chain
    ContinueOnStop() bool // run even after the execution was already stopped?
}
```

Attach middleware globally via `Scheduler.Use(...)` or per-job via
`Job.Use(...)`; global middleware propagates to every job. Middleware is
deduplicated by type, so attaching the same middleware twice is a no-op —
implementations that legitimately need multiple instances (e.g. several
webhook destinations) opt into per-instance dedup via a `Key() string` method.
Ofelia provides built-in middleware for logging via mail, saving execution
reports, sending Slack messages and webhooks, deduplicating overlapping runs,
and skipping already-running jobs. Custom middleware can be added by
implementing the interface in `core` and attaching it to the scheduler or to
individual jobs. See `core/common.go` (`Context.Next`, `Middleware`) and
`middlewares/` for the built-in implementations.

## HTTP interfaces

Ofelia can optionally expose a small web UI and Go's `pprof` debug server. Both
servers are configured in the `[global]` section of the INI file or through
Docker labels on the service container:

```ini
[global]
enable-web = true
web-address = :8081
enable-pprof = true
pprof-address = 127.0.0.1:8080
```

The equivalent labels are `ofelia.enable-web`, `ofelia.web-address`,
`ofelia.enable-pprof` and `ofelia.pprof-address`.

The web UI exposes `/api/jobs` for active jobs, `/api/jobs/removed` for those
that have been deregistered and `/api/jobs/{name}/history` with the execution
history of a specific job, including stdout and stderr for each run.
`/api/config` returns the currently active configuration.
Jobs can also be created, updated or deleted via `/api/jobs/create`,
`/api/jobs/update` and `/api/jobs/delete`. The request body accepts the job
`name`, `type` (`local`, `run`, `exec` or `compose`), `schedule`, `command` and
any additional options such as `image`, `container`, `file` or `service`.
`run` and `exec` jobs require the server to be started with a Docker client.
Attempts to create them without a Docker client will return an error.

