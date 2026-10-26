# ADR-005: Durable Schedule State for Compose Job Triggers

**Status**: Accepted  
**Date**: 2026-10-24  
**Authors**: SME

## Context

[ADR-004](./ADR-004-compose-jobs-as-job-source.md) imports the `jobs:` of registered Compose projects and accepts a subset of their triggers. Two settings of the [Compose specification](https://github.com/compose-spec/compose-spec/blob/6263066d8604bfef328f65a8208e4183bb2187a6/jobs.md) are rejected, because Ofelia keeps no state that could implement them:

- `missed_fires` is "the policy applied to fires missed while the platform was unavailable — `one` (the default) runs a single catch-up, `skip` ignores them".
- `concurrency` is "the policy applied when the schedule fires while a previous run is still in progress — `forbid` (the default) prevents the new run, `queue` runs it once the current one completes". The Engine implementation ([moby/moby#53485](https://github.com/moby/moby/pull/53485)) keeps at most one pending fire.

Because `missed_fires: one` is the default, the plain cron form (`schedule: ["0 2 * * *"]`) does not load today.

What exists:

1. **go-cron** (`github.com/netresearch/go-cron` v0.16.1, maintained by Netresearch) can catch up missed runs. `WithMissedPolicy(MissedRunOnce)` runs the job once for the last missed scheduled time it counts between the entry's previous run and now. It needs that previous run through `WithPrev(t)`; without it no catch-up happens. go-cron stores nothing (`missed.go`: "The scheduler does NOT persist state"). The catch-up runs whenever an entry is added: at scheduler start and for every entry added later. It does not check whether the entry is paused. It stops counting after 100 missed times and then runs the last one it counted, so after a long downtime it does not run the most recent missed time.
2. go-cron passes the scheduled time of a run to its observability hooks (`OnJobStart`) only. A job does not receive it. The hooks fire before the job wrappers run, so they also fire for a run that a wrapper then drops.
3. **Ofelia's scheduler** (`core/scheduler.go`) wraps every job in a global concurrency limit (default 10, `maxConcurrentSkipWrapper`). A run that finds the limit full is dropped before an `Execution` exists and is logged at debug level only. `no-overlap` (`middlewares/overlap.go`) stops a run of a job that is already running and records it as skipped.
4. **The state file** (`--state-file`, `core/persist`) is written atomically (temporary file, then rename). It stores jobs created through the API and the set of disabled jobs. Its documentation puts runtime data such as history and last run out of scope.
5. **Reload** (`cli/config.go`, `replaceIfChanged`) removes and re-adds a job whose hash changed. Removing the go-cron entry cancels the context of a run in progress, so a definition change cancels that run. `RemoveJob` does not wait for that run to end: it calls `WaitForJobByName` after the removal, which no longer finds the name and returns at once. The re-added entry starts without a previous run. It also starts enabled: `RemoveJob` forgets that the job was disabled.
6. **Start-up order** (`cli/daemon.go`): jobs are registered first, and the state file is loaded and its disabled set applied afterwards. go-cron runs catch-ups when the scheduler starts.
7. **Shutdown** cancels running jobs and waits up to 30 seconds. Nothing is written at shutdown.
8. **A manual run** (`TriggerEntryByName`) reaches the job through the same path as a scheduled fire, with the current time as its scheduled time.

## Decision

### The schedule cursor

Ofelia keeps one record per **schedule entry**: a job identity plus a trigger position, as ADR-004 defines them. The record holds:

| Field | Meaning |
|-------|---------|
| Schedule revision | Hash of the cron expression, the resolved time zone, `concurrency` and `missed_fires` |
| Activation time | When this schedule revision was first armed |
| Cursor | The most recent scheduled time the scheduler evaluated while it was running |
| Dispatch | The occurrence that was admitted last: its scheduled time, its trigger kind (scheduled, catch-up, manual), its execution ID, its state (waiting for a global slot, started, ended) and, once it ended, its outcome |
| Pending | The occurrence in the queue slot, if any (`concurrency: queue` only) |
| Workload | The execution ID whose workload has not been confirmed ended, independently of the dispatch's outcome |

The cursor advances on **every** fire the running scheduler evaluates, including fires that `forbid` stops or the queue coalesces. A fire is missed only when the scheduler was not running at its scheduled time. Without this rule, a reload that re-adds a job would treat a fire that `forbid` skipped five minutes earlier as missed and run it. A paused go-cron entry is rescheduled before any job code runs, so a disabled job does not advance the cursor; enabling it resets the cursor instead (see below).

Advancing the cursor and recording what happened to that fire are **one atomic update** of the record. The outcomes are: admitted (dispatch), queued (pending), skipped by `forbid`, coalesced. A fire that has to wait for a global slot is admitted with the dispatch state "waiting" at the moment the cursor passes it; it occupies the job, not the queue slot. So no crash can leave a fire that the cursor has passed but that is neither admitted, queued nor recorded as skipped.

A fire is identified by its scheduled time in UTC, not by the time it started.

A manual run (`TriggerEntryByName`, the API, the web UI) does not advance the cursor. The run-on-startup run counts as one: it starts at the time the job is registered, which the schedule never produces. It is refused while the entry is busy, that is while a run is in progress or a dispatch waits for a global slot, so it never replaces a scheduled dispatch. An admitted manual run is recorded in the dispatch field with the trigger kind manual, so the crash rule below covers it too. While it runs, it occupies the job like a scheduled run: `forbid` stops a scheduled fire and `queue` puts it into the pending slot.

### When a catch-up runs

When an entry is registered, Ofelia compares the stored record with the configuration:

| Situation | Effect |
|-----------|--------|
| No record | **First registration.** The record is written durably with activation time = cursor = now, and no catch-up runs. Fires before activation are never missed; a fire after it that falls into a later downtime is caught up, because the cursor is set. |
| Record, same schedule revision | **Downtime.** Ofelia computes the most recent scheduled time `t` with `cursor < t <= now`. If there is one, a single catch-up with that scheduled time starts through Ofelia's own dispatch path, the one a pending occurrence takes. Admitting it sets the cursor to that time in the same atomic update as its dispatch, so a second restart does not compute it again. |
| Record, different schedule revision | **Schedule change.** Treated as first registration. A time that the old expression produced is not a missed fire of the new one, and a pending occurrence accepted under `queue` is not valid under `forbid`, so a pending or waiting occurrence of the old revision is dropped and logged. |
| Job disabled | **Deliberate disable.** The entry is added paused, no catch-up is computed, and nothing of it starts while the job is disabled. Disabling drops a pending or waiting occurrence and logs it. Enabling the job sets the cursor to the enable time, so fires during the disabled period never count as missed. |
| Job definition changed, schedule unchanged | The record is kept, because it is keyed by the schedule entry and not by the go-cron entry. The reload cancels a run in progress, as it does today; that occurrence ends with the outcome canceled. A pending occurrence, and a scheduled or catch-up dispatch still waiting for a global slot, stay in the record and start on the new entry, but only after the canceled run of the old entry has ended. A manual dispatch still waiting is dropped and logged. A disabled job stays disabled. |

The comparison happens before the go-cron entry is live, so no fire can see a record that has not been evaluated: for jobs registered at start-up it runs when the scheduler starts, which is after the state file, with its disabled set and its records, has been loaded; for a job added later it runs before its entry is added. Only the start of the chosen occurrence waits until the entry is live. A reload keeps the disabled state of a job it replaces. A reload does not wait for the replaced entry's run to end; the per-entry check keeps the entry busy until that run has recorded its end, so nothing carried over starts beside it.

go-cron's `MissedRunOnce` is not used. It stops counting after 100 missed times, so after a long downtime it runs an old scheduled time and moves the cursor there, and it does not check whether an entry is paused.

There is no grace period: the specification defines none. A daemon that was down for a week runs one catch-up per schedule entry, not seven.

**At most one start per schedule entry happens at registration.** A stored pending occurrence, or a scheduled or catch-up dispatch that was still waiting for a global slot and so never started, counts as the catch-up: it starts, and no computed catch-up is added beside it. A manual dispatch that was still waiting is dropped and logged; it never replaces the catch-up.

`missed_fires: skip` keeps today's behavior and needs no record.

### Dispatch is recorded before the run starts

For a schedule entry with `missed_fires: one` or `concurrency: queue`, Ofelia writes the dispatch record durably before it starts the workload, and writes the outcome when the run ends. The guarantee is **at most once per occurrence**:

- A crash before the write leaves the fire unrecorded. After the restart it counts as missed and runs as the catch-up. It had not started, so it does not run twice.
- The state "started" is written immediately before the workload is launched. A crash between that write and the launch therefore loses the occurrence: it counts as handled with an unknown outcome although nothing ran. This is the price of at most once; the opposite order could run it twice.
- A crash after the run started and before it finished leaves a started dispatch with no recorded end. After the restart that occurrence counts as handled and is not started again. Its outcome is recorded as unknown, in the same terms as `ErrExecOutcomeUnknown` for exec jobs.
- For `run`-mode Compose jobs, the `ofelia.execution` label identifies that workload's container. The unresolved workload execution ID is persisted separately from the dispatch's outcome, so repeated restarts and a changed schedule preserve the check. While its container still runs, or Docker cannot be queried, the entry counts as busy: `forbid` skips the catch-up and later fires, `queue` holds them in the slot. A confirmed end clears the workload ID durably before a queued occurrence can start. Older records with a started or unsuccessful dispatch are checked as well. For `exec` mode no such check exists, so the restarted daemon can start a second run beside a command that is still running.

- A run that shutdown cancels ends with the outcome canceled. It counts as handled and is not started again after the restart. A run that has not ended when the 30-second shutdown wait expires has no recorded end, so after the restart it follows the crash rule: outcome unknown, and for `run` mode the entry stays busy while its labelled container runs.
- If Compose cancellation returns `ErrComposeTerminationUnconfirmed`, the execution records its failed or canceled outcome but the job stays busy until a later container check confirms the workload has ended. That protection survives daemon restarts. The failed execution is not retried, and a queued or manual run cannot overlap the unresolved workload.
- A dispatch that is still waiting for a global slot has not started, so cancellation does not end it. On shutdown or reload the wait returns at once and the dispatch keeps the state "waiting"; it then starts as the catch-up or on the new entry, as after a crash. Two cases drop it and log it instead: a manual dispatch, and any waiting dispatch after a schedule change. A goroutine that gets its slot at the moment it is canceled checks for cancellation again and does not start the workload, so the occurrence cannot start on the old and the new entry.

Running an occurrence twice would be the worse failure for jobs such as backups, rotations and migrations.

### The queue

`concurrency: queue` gets one pending slot per schedule entry:

- A fire while the job runs, or while its admitted occurrence waits for a global slot, fills an empty slot. The pending occurrence is written to the record.
- A fire while the slot is already full is dropped and logged as coalesced. This matches the Engine's single pending fire.
- When the run ends, the pending occurrence starts at once and the slot empties.

go-cron's `DelayIfStillRunning` is not used. It queues without a limit, so a long run would collect one waiting goroutine per fire.

### The global limit delays, it does not drop

For Compose-sourced schedule entries with `missed_fires: one` or `concurrency: queue`, the global concurrency limit no longer drops a fire. A fire that cannot get a slot waits for one, and while it waits the schedule entry counts as busy: `forbid` stops later fires and `queue` puts them into the pending slot. The catch-up run follows the same rule. Without this, a catch-up or a queued run could be dropped by a limit the user never configured, and the drop would be visible only in debug logs.

This needs a different order of checks. Today the global limit is a go-cron job wrapper (`core/scheduler.go`), and `no-overlap` is a middleware inside the job, so a second fire reaches the limit before the overlap check sees it. For these entries, the per-entry check — busy, pending slot, `forbid` — runs first, and only an admitted fire asks the global limit for a slot.

Other Compose entries and Ofelia's own job types keep the current limit behavior. Changing them is a separate decision.

### Where the state lives

The records are a new section of the existing state file. `missed_fires: one` and `concurrency: queue` require `--state-file`. Without it, a project using either setting is rejected with an error that names the flag. Silently degrading to `skip` or `forbid` would be the compatibility bug ADR-004 rules out.

After a successful load, Ofelia removes records of schedule entries that no longer exist. Records of a rejected project stay, as its last loaded jobs do.

A state file has one owner. Two daemons sharing one file are not supported, consistent with ADR-004's single scheduling owner.

### Change in go-cron

The record must be written by the code that admits or drops the run, with the run's scheduled time. go-cron gives a job no way to read that time. go-cron puts both into the context a job receives (`cron.RunInfoFromContext`): the scheduled time and the kind of trigger, one of scheduled, manual, catch-up (go-cron's own `MissedPolicy`, which Ofelia does not use), dependency (a workflow child) and immediate (the first run of an entry added `WithRunImmediately`, Ofelia's run-on-startup). The kind is needed because a manual run arrives with the current time as its scheduled time, and it must not advance the cursor. go-cron already uses the context this way for the workflow execution ID of entries with dependents.

Ofelia starts a catch-up, a queued occurrence and a dispatch that waited for a slot through its own dispatch path: it records the occurrence as a waiting dispatch, then starts the entry with `TriggerEntryWithInfo`, which carries that occurrence's scheduled time and kind. The run arrives at the per-entry check as what it is, and the check matches it to the waiting dispatch; no side state in memory is needed to recognize it, and a start that go-cron refuses leaves the dispatch waiting for the next registration.

Both are additive changes in a module Netresearch maintains ([netresearch/go-cron#429](https://github.com/netresearch/go-cron/pull/429)).

`RemoveJob` does not wait for the run it cancels. The in-process state of an entry is keyed by the job name and outlives the go-cron entry, so when the canceled run kept a record too, the replacement's first fire finds the entry busy while that run is still stopping, and follows the concurrency policy like any other fire. When the canceled run belonged to a job that kept no record, as after a reload that adds `missed_fires` or `concurrency`, the per-entry check does not know that run; the fire is then skipped until the run has stopped, as for any job without a record.

## Consequences

### Positive

- The specification's defaults load: the plain cron form works once a state file is configured.
- A restart, a reload or a deploy no longer loses a scheduled run silently.
- A fire is never dropped without a record: `forbid`, the queue and the global limit each leave a log entry at a level an operator sees.

### Negative

- Each fire of an affected schedule entry costs atomic writes of the state file: two for a run (dispatch and outcome), a third when the run first waits for a global slot (waiting, then started), one for a fire that is skipped or coalesced. At a one-minute schedule that is two, at most three, file renames per minute per job.
- A crash during a run makes its outcome unknown rather than retried. An operator must check the occurrence by hand.
- After a crash, an `exec`-mode Compose job can run twice at the same time, because the restarted daemon cannot see the command that is still running.
- The implementation depends on a go-cron release with the changes above.

### Neutral

- Schedules with `missed_fires: skip` and `concurrency: forbid` behave as they do today.
- More than one schedule entry per job stays rejected. The records are per entry, but overlap state is still per job.
- Cleanup of orphaned containers after a crash is not part of this decision. The execution label makes it possible later.

## Implementation notes

What the implementation does where the context above, written before it, describes an earlier state:

- `RemoveJob` still does not wait for the canceled run, but a job registered under the same name no longer runs beside it ([#896](https://github.com/netresearch/ofelia/pull/896)): a job without a record skips its fires until that run has stopped, and a job with a record counts it as busy when that run kept a record too; otherwise its fires are skipped the same way. Context item 5 predates that.
- A reload already kept the disabled state of a job it replaces (`7fabd9a`) when this ADR was written; context item 5 predates that too.
- The start-up order is unchanged: jobs are still registered before the state file is loaded. The comparison runs when the scheduler starts, which is after the load (see "When a catch-up runs").
- A schedule without a time zone is evaluated in the scheduler's local zone; the record keeps times in UTC, and the catch-up search converts the cursor back before it evaluates the schedule.
- The schedule revision is a hash of the cron expression (which carries `CRON_TZ` for an explicit zone), `concurrency`, `missed_fires` and, when the expression names no zone, the local zone: its name and its abbreviation and UTC offset in January and July. The name alone is `Local` when the zone comes from `/etc/localtime`, so a changed host zone would otherwise keep the record.
- `ofelia validate` takes `--state-file` (and `OFELIA_STATE_FILE`) to apply the same requirement as the daemon; commands that only inspect the configuration skip it.

## References

- [ADR-004](./ADR-004-compose-jobs-as-job-source.md) - Compose jobs as a job source
- [Compose specification: jobs](https://github.com/compose-spec/compose-spec/blob/6263066d8604bfef328f65a8208e4183bb2187a6/jobs.md)
- [moby/moby#53485](https://github.com/moby/moby/pull/53485) - Engine jobs (experimental)
- [go-cron `missed.go`](https://github.com/netresearch/go-cron/blob/v0.16.1/missed.go) - missed-run policies
- [#593](https://github.com/netresearch/ofelia/issues/593) - state file
