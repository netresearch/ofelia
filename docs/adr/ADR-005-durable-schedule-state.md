# ADR-005: Durable Schedule State for Compose Job Triggers

**Status**: Proposed  
**Date**: 2026-10-24  
**Authors**: SME

## Context

[ADR-004](./ADR-004-compose-jobs-as-job-source.md) imports the `jobs:` of registered Compose projects and accepts a subset of their triggers. Two settings of the [Compose specification](https://github.com/compose-spec/compose-spec/blob/6263066d8604bfef328f65a8208e4183bb2187a6/jobs.md) are rejected, because Ofelia keeps no state that could implement them:

- `missed_fires` is "the policy applied to fires missed while the platform was unavailable — `one` (the default) runs a single catch-up, `skip` ignores them".
- `concurrency` is "the policy applied when the schedule fires while a previous run is still in progress — `forbid` (the default) prevents the new run, `queue` runs it once the current one completes". The Engine implementation ([moby/moby#53485](https://github.com/moby/moby/pull/53485)) keeps at most one pending fire.

Because `missed_fires: one` is the default, the plain cron form (`schedule: ["0 2 * * *"]`) does not load today.

What exists:

1. **go-cron** (`github.com/netresearch/go-cron` v0.16.1, maintained by Netresearch) can catch up missed runs. `WithMissedPolicy(MissedRunOnce)` runs the job once for the most recent scheduled time between the entry's previous run and now. It needs that previous run through `WithPrev(t)`; without it no catch-up happens. go-cron stores nothing (`missed.go`: "The scheduler does NOT persist state"). The catch-up runs whenever an entry is added: at scheduler start and for every entry added later. It does not check whether the entry is paused.
2. go-cron passes the scheduled time of a run to its observability hooks (`OnJobStart`) only. A job does not receive it. The hooks fire before the job wrappers run, so they also fire for a run that a wrapper then drops.
3. **Ofelia's scheduler** (`core/scheduler.go`) wraps every job in a global concurrency limit (default 10, `maxConcurrentSkipWrapper`). A run that finds the limit full is dropped before an `Execution` exists and is logged at debug level only. `no-overlap` (`middlewares/overlap.go`) stops a run of a job that is already running and records it as skipped.
4. **The state file** (`--state-file`, `core/persist`) is written atomically (temporary file, then rename). It stores jobs created through the API and the set of disabled jobs. Its documentation puts runtime data such as history and last run out of scope.
5. **Reload** (`cli/config.go`, `replaceIfChanged`) removes and re-adds a job whose hash changed. The re-added go-cron entry starts without a previous run.
6. **Shutdown** cancels running jobs and waits up to 30 seconds. Nothing is written at shutdown.

## Decision

### The schedule cursor

Ofelia keeps one record per **schedule entry**: a job identity plus a trigger position, as ADR-004 defines them. The record holds:

| Field | Meaning |
|-------|---------|
| Schedule revision | Hash of the cron expression and the resolved time zone |
| Activation time | When this schedule revision was first armed |
| Cursor | The most recent scheduled time the scheduler evaluated while it was running |
| Dispatch | The last occurrence that was started: its scheduled time, its execution ID and, once it ended, its outcome |
| Pending | The occurrence that is accepted but not yet started, if any |

The cursor advances on **every** fire the running scheduler evaluates, including fires that `forbid` stops or the queue coalesces. A fire is missed only when the scheduler was not running at its scheduled time. Without this rule, a reload that re-adds a job would treat a fire that `forbid` skipped five minutes earlier as missed and run it. A paused go-cron entry is rescheduled before any job code runs, so a disabled job does not advance the cursor; enabling it resets the cursor instead (see below).

Advancing the cursor and recording what happened to that fire are **one atomic update** of the record. The outcomes are: dispatched, pending, skipped by `forbid`, coalesced. A fire that has to wait for a global slot is recorded as pending at the moment the cursor passes it. So no crash can leave a fire that the cursor has passed but that is neither started, pending nor recorded as skipped.

A fire is identified by its scheduled time in UTC, not by the time it started.

A manual run (`TriggerEntryByName`, the API, the web UI) does not advance the cursor. While it runs, it occupies the job like a scheduled run: `forbid` stops a scheduled fire and `queue` puts it into the pending slot.

### When a catch-up runs

When an entry is registered, Ofelia compares the stored record with the configuration:

| Situation | Effect |
|-----------|--------|
| No record | **First registration.** Activation time = now, no catch-up. Fires before activation are never missed. |
| Record, same schedule revision | **Downtime.** The entry is added with `WithPrev(cursor)` and `MissedRunOnce`. At most one catch-up runs, for the most recent missed time. |
| Record, different schedule revision | **Schedule change.** Treated as first registration. A time that the old expression produced is not a missed fire of the new one. |
| Job disabled | **Deliberate disable.** The entry is added without a missed policy. Enabling the job sets the cursor to the enable time, so fires during the disabled period never count as missed. |
| Job definition changed, schedule unchanged | The record is kept. A catch-up runs with the new definition, consistent with ADR-004: a reload affects later executions only. |

There is no grace period: the specification defines none. A daemon that was down for a week runs one catch-up per schedule entry, not seven.

**At most one start per schedule entry happens at registration.** A stored pending occurrence counts as the catch-up: it starts, and no `MissedRunOnce` run is added beside it. Only an entry without a pending occurrence gets the missed policy.

`missed_fires: skip` keeps today's behavior and needs no record.

### Dispatch is recorded before the run starts

For a schedule entry with `missed_fires: one` or `concurrency: queue`, Ofelia writes the dispatch record durably before it starts the workload, and writes the outcome when the run ends. The guarantee is **at most once per occurrence**:

- A crash before the write leaves the fire unrecorded. After the restart it counts as missed and runs as the catch-up. It had not started, so it does not run twice.
- A crash after the write and before the run finishes leaves a dispatch with no recorded end. After the restart that occurrence counts as handled and is not started again. Its outcome is recorded as unknown, in the same terms as `ErrExecOutcomeUnknown` for exec jobs. For `run`-mode Compose jobs, the `ofelia.execution` label on the container identifies the possibly orphaned workload.

- A run that shutdown cancels ends with the outcome canceled. It counts as handled and is not started again after the restart.

Running an occurrence twice would be the worse failure for jobs such as backups, rotations and migrations.

### The queue

`concurrency: queue` gets one pending slot per schedule entry:

- A fire while the job runs fills an empty slot. The pending occurrence is written to the record.
- A fire while the slot is already full is dropped and logged as coalesced. This matches the Engine's single pending fire.
- When the run ends, the pending occurrence starts at once and the slot empties.

go-cron's `DelayIfStillRunning` is not used. It queues without a limit, so a long run would collect one waiting goroutine per fire.

### The global limit delays, it does not drop

For Compose-sourced schedule entries, the global concurrency limit no longer drops a fire. A fire that cannot get a slot waits for one, and while it waits the schedule entry counts as busy: `forbid` stops later fires and `queue` puts them into the pending slot. The catch-up run follows the same rule. Without this, a catch-up or a queued run could be dropped by a limit the user never configured, and the drop would be visible only in debug logs.

This needs a different order of checks. Today the global limit is a go-cron job wrapper (`core/scheduler.go`), and `no-overlap` is a middleware inside the job, so a second fire reaches the limit before the overlap check sees it. For Compose-sourced entries, the per-entry check — busy, pending slot, `forbid` — runs first, and only an admitted fire asks the global limit for a slot.

Ofelia's own job types keep the current limit behavior. Changing them is a separate decision.

### Where the state lives

The records are a new section of the existing state file. `missed_fires: one` and `concurrency: queue` require `--state-file`. Without it, a project using either setting is rejected with an error that names the flag. Silently degrading to `skip` or `forbid` would be the compatibility bug ADR-004 rules out.

After a successful load, Ofelia removes records of schedule entries that no longer exist. Records of a rejected project stay, as its last loaded jobs do.

A state file has one owner. Two daemons sharing one file are not supported, consistent with ADR-004's single scheduling owner.

### Change in go-cron

The record must be written by the code that admits or drops the run, with the run's scheduled time. go-cron gives a job no way to read that time. go-cron will put the scheduled time into the context a job receives, beside the workflow execution ID it already carries. This is an additive change in a module Netresearch maintains.

## Consequences

### Positive

- The specification's defaults load: the plain cron form works once a state file is configured.
- A restart, a reload or a deploy no longer loses a scheduled run silently.
- A fire is never dropped without a record: `forbid`, the queue and the global limit each leave a log entry at a level an operator sees.

### Negative

- Each fire of an affected schedule entry costs atomic writes of the state file: two for a run (dispatch and outcome), one for a fire that is skipped or coalesced. At a one-minute schedule that is up to two file renames per minute per job.
- A crash during a run makes its outcome unknown rather than retried. An operator must check the occurrence by hand.
- A go-cron release must ship before the implementation.

### Neutral

- Schedules with `missed_fires: skip` and `concurrency: forbid` behave as they do today.
- More than one schedule entry per job stays rejected. The records are per entry, but overlap state is still per job.
- Cleanup of orphaned containers after a crash is not part of this decision. The execution label makes it possible later.

## References

- [ADR-004](./ADR-004-compose-jobs-as-job-source.md) - Compose jobs as a job source
- [Compose specification: jobs](https://github.com/compose-spec/compose-spec/blob/6263066d8604bfef328f65a8208e4183bb2187a6/jobs.md)
- [moby/moby#53485](https://github.com/moby/moby/pull/53485) - Engine jobs (experimental)
- [go-cron `missed.go`](https://github.com/netresearch/go-cron/blob/v0.16.1/missed.go) - missed-run policies
- [#593](https://github.com/netresearch/ofelia/issues/593) - state file
