# Ofelia

Ofelia schedules jobs that run in Docker containers, in Compose projects or on the host. This glossary names the concepts of how a job comes into being.

## Language

**Job**:
A named unit of work with a schedule that the scheduler runs.
_Avoid_: task, cron entry

**Job definition**:
The description of a job as one job source states it, before Ofelia's own rules and the global settings are applied to it.
_Avoid_: job config, raw job

**Job source**:
Where a job definition comes from: the INI file, container labels, a Compose project, or the web API. A job restored from the state file keeps the job source it was created from.
_Avoid_: origin, provenance

**Job assembly**:
The step that turns a job definition into a job. It applies the global settings, such as the default user, and the rules a job must meet.
_Avoid_: job preparation, finishing, prep

**Default user**:
The user a container job runs as when its definition names none, set by `[global] default-user`.
_Avoid_: fallback user
