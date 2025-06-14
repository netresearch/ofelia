# Architecture Overview

Ofelia is built around a pluggable scheduler that manages different job types and middleware.

## Scheduler

The scheduler wraps [robfig/cron](https://github.com/robfig/cron) and keeps track of all registered jobs. It runs them according to their cron expressions and exposes methods to start or stop execution. Each job is wrapped so execution metadata and middleware can be handled.

## Job types

Jobs implement a common interface and can be executed in several ways:

- **`exec`** – run a command inside an existing container similar to `docker exec`.
- **`run`** – start a new container for every execution.
- **`local`** – execute a command on the host where Ofelia runs.
- **`service-run`** – run a one‑off service in a Docker swarm.

## Middleware

Middleware hooks are executed around every job. Ofelia provides built-in middleware for logging via mail, saving execution reports and sending Slack messages. Custom middleware can be added by implementing the interface in `core` and attaching it to the scheduler or to individual jobs.

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

