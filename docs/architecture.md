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

