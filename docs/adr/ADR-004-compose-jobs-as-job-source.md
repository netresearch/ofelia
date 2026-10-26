# ADR-004: Compose Jobs as a Job Source

**Status**: Proposed  
**Date**: 2026-10-24  
**Authors**: SME

## Context

Docker Compose 5.6.0 (released 2026-10-02) added a top-level `jobs:` section to the Compose file format. A job carries the same container and workload configuration as a service, plus `triggers`: `manual` and a list of `schedule` entries with `cron`, `timezone`, `concurrency` and `missed_fires`. The released Compose implements only manual execution:

- `docker compose run <job>` runs a job. It refuses a job declared with `triggers.manual: false`, and it applies the same check to every job in the target's `depends_on` closure.
- `docker compose up` fails before creating anything when an active job has a schedule (`scheduled jobs are not supported in this version`). A job behind an inactive profile does not count as active.

Native scheduling is being built into Docker Engine ([moby/moby#53485](https://github.com/moby/moby/pull/53485), open and marked experimental) and into Compose ([docker/compose#14155](https://github.com/docker/compose/pull/14155), draft). Neither is released. The Engine pull request accepts one trigger per job, keeps at most one pending fire for `concurrency: queue`, and evaluates an empty `timezone` as UTC. The Compose specification evaluates an empty `timezone` in the platform's local time zone, and defaults `concurrency` to `forbid` and `missed_fires` to `one`.

Ofelia already runs Compose workloads through `job-compose`, which shells out to `docker compose -f <file> run --rm <service>` or `exec`. Since [#887](https://github.com/netresearch/ofelia/pull/887), a canceled `run` execution stops and removes its container, found through the `ofelia.execution` label, and in `run` mode the CLI is pointed at the same Docker endpoint as Ofelia's own client. `job-compose` does not read Compose job definitions or their triggers: it knows a file, a service name and a command.

Two existing facts constrain the design:

1. Issue [#758](https://github.com/netresearch/ofelia/issues/758) lists `project`, `dir` and `environment` on `job-compose` as out of scope, so that Ofelia does not become a second container configuration language beside Compose.
2. Ofelia's persistence (`core/persist`, `middlewares/save.go`, `middlewares/restore.go`) stores the definitions of jobs created through the API, the set of disabled jobs and, with `save-folder`, completed execution history. It stores no scheduling cursor, and restored executions are never marked as running. It cannot implement a catch-up policy today.

## Decision

### Compose is a source of job definitions

Ofelia loads Compose projects and discovers their `jobs:`. The workload definition stays in Compose: image, command, environment, mounts and networks are not repeated in Ofelia configuration.

A **registered Compose project** is the canonical execution context. It holds the ordered Compose files, the selected profiles and the env file selection. Its Docker endpoint is the one Ofelia's own client uses, to which the Compose CLI is pinned since #887. Jobs reference the project; the context is not repeated on every job.

This revises the scope that [#758](https://github.com/netresearch/ofelia/issues/758) describes, for keys that **select** a Compose project only. Keys that **configure containers** (DNS, labels, restart policy and the like) stay out of scope, for the reason #758 gives. For jobs loaded from Compose, resource limits are set in the Compose file as well.

Existing `job-compose` entries keep working unchanged.

### Identity

| Object | Identity |
|--------|----------|
| Project | Resolved Docker endpoint + Compose project name |
| Job | Project identity + job name |
| Schedule entry | Job identity + trigger position |

A Docker context name is an alias for an endpoint and is never part of an identity. Launch, observation and cleanup resolve to the same endpoint. While Ofelia talks to a single endpoint, the endpoint part is implicit and the job name is `<registration>.<job>`; it becomes explicit once more than one endpoint is supported.

Registration names are display aliases, not separate workload identities. If multiple registrations schedule the same job in the same resolved Compose project, Ofelia rejects every colliding registration atomically. This check also covers definitions retained after a rejected reload; retained and newly loaded jobs cannot independently schedule one canonical workload. Distinct job names in one project and jobs in distinct projects remain independent.

Each job carries the revision of its resolved definition and of the jobs and services in its `depends_on` closure, which `docker compose run` starts with it, and each execution records it. A reload replaces only the jobs whose definition changed, so a change to one job does not reset history or overlap state of the project's other jobs. The revision of the whole loaded project is logged when it loads. A reload affects later executions only.

### One scheduling owner per job

Every job has exactly one scheduling owner: Ofelia, or, once a released Engine supports it, the Engine. Detecting that a daemon supports native jobs never moves ownership by itself; moving a job between owners is an explicit operation that accounts for in-flight and pending executions.

### Loader

Ofelia uses [compose-go](https://github.com/compose-spec/compose-go), the reference implementation of the Compose specification, behind a small interface, so that execution stays a separate adapter. It handles interpolation, path resolution, profiles and merging.

Reading the normalized output of `docker compose config` is not used. It needs the docker CLI and the Compose plugin at load time, which the default image does not contain, and Ofelia would parse the Compose model a second time outside compose-go. Its JSON form also drops the `x-` fields of jobs: compose-go v2.16.1 tags the `Extensions` of `JobConfig`, `TriggerConfig` and `ScheduleConfig` with `json:"-"` ([types/jobs.go](https://github.com/compose-spec/compose-go/blob/v2.16.1/types/jobs.go)). The default YAML form keeps them.

### Initial trigger subset

Ofelia accepts a documented subset and rejects everything else with an error that names the unsupported setting. Silently mapping a setting to a different behavior is a compatibility bug.

Accepted:

- Five-field cron expressions, kept separate from Ofelia's own cron extensions and interval schedules.
- `timezone`. An empty value is resolved to the local time zone of the scheduling platform, as the specification defines, and the resolved zone is recorded and displayed.
- `concurrency: forbid`, explicit or by default. The importer enables the existing per-job overlap prevention (`no-overlap`, `middlewares/overlap.go`).
- `missed_fires: skip`, only when set explicitly.

Rejected until the deferred work below exists:

- `missed_fires: one`, explicit or by default. The plain cron form (`schedule: ["0 2 * * *"]`) therefore fails; the error tells the user to set `missed_fires: skip` if that behavior is acceptable. `run-on-startup` is not a catch-up implementation.
- `concurrency: queue`.
- `triggers.manual: false` on the job or on any job in its `depends_on` closure, while execution goes through `docker compose run`, which refuses such jobs.
- More than one schedule entry per job, until overlap state can be shared across the entries of one job.

## Consequences

### Positive

- Workload configuration is written once, in Compose.
- The supported behavior is explicit: a Compose job either runs as specified or its project is rejected at load time with a logged error. Other projects and jobs are unaffected, as for any rejected Ofelia job.
- The identity model keeps history, overlap prevention and cleanup correct when several projects define jobs with the same name.

### Negative

- compose-go is a new dependency of substantial size.
- The specification's default schedule (`missed_fires: one`) is rejected in the first version, so the shortest valid Compose schedule does not load.
- Two configuration paths for Compose workloads exist side by side: `job-compose` and registered projects.

### Neutral

- Execution keeps delegating to the Compose CLI. Reimplementing Compose's container lifecycle is out of scope.
- Deferred to later decisions:
  - Durable scheduling and dispatch state (activation time, accepted occurrence, dispatch started, one queued occurrence), which `missed_fires: one` and `concurrency: queue` need.
  - A native Engine backend: production support requires a released Engine capability, a compatibility matrix and runtime detection; observation of Engine-owned jobs comes first.
  - Cancellation semantics for commands executed inside existing containers (`job-exec`, `job-compose` with `exec`): the Engine has no way to stop an exec'd process, so the client can stop waiting while the command keeps running.

## References

- [Compose specification: jobs](https://github.com/compose-spec/compose-spec/blob/main/jobs.md)
- [Compose 5.6.0 release](https://github.com/docker/compose/releases/tag/v5.6.0); `rejectScheduledJobs` in [cmd/compose/up.go](https://github.com/docker/compose/blob/v5.6.1/cmd/compose/up.go); `materializeManualJob` in [cmd/compose/run.go](https://github.com/docker/compose/blob/v5.6.1/cmd/compose/run.go)
- [moby/moby#53485](https://github.com/moby/moby/pull/53485) - Engine jobs (experimental); [trigger contract](https://github.com/moby/moby/blob/b9b13164743e02339e43d063a67ad2fe57da9b3f/extpoints/jobs/api/v0/jobs.go)
- [docker/compose#14155](https://github.com/docker/compose/pull/14155) - Compose integration with Engine jobs (draft)
- [#758](https://github.com/netresearch/ofelia/issues/758) - container configuration keys on `job-compose`
- [#887](https://github.com/netresearch/ofelia/pull/887) - cancellation of `job-compose` run executions
- [ADR-002](./ADR-002-security-boundaries.md) - security boundaries
