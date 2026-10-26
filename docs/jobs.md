# Jobs reference

- [`exec`](#exec)
- [`run`](#run)
- [`local`](#local)
- [`service-run`](#service-run)
- [`compose`](#compose)

## `exec`

This job is executed inside a running container, similar to `docker exec`.

### Parameters

- **`schedule`: string**
  - When the job should be executed. E.g. every 10 seconds or every night at 1 AM.
- **`command`: string**
  - Command you want to run inside the container.
- **`container`: string**
  - Name of the container you want to execute the command in.
- `user`: string = `nobody`
  - User as which the command should be executed, similar to `docker exec --user <user>`
  - If not set, inherits the global `default-user` (default `nobody`) — an empty value here means "inherit", not "container default". Set `user = default` for the container's own default user regardless of the global; `default` is reserved for that meaning and cannot name a real user
- `tty`: boolean = `false`
  - Allocate a pseudo-tty, similar to `docker exec -t`. See this [Stack Overflow answer](https://stackoverflow.com/questions/30137135/confused-about-docker-t-option-to-allocate-a-pseudo-tty) for more info.
- `console-height`: uint = `0`
  - Initial pseudo-TTY console size in rows. Only honored by the Docker daemon when `tty = true`; otherwise silently ignored. `0` means "use Docker's default size". Useful for jobs that render TUIs or formatted text that expects a specific terminal geometry (`htop`, `vim`, tables). Requires Docker API v1.42+ (Docker Engine 20.10+). See [#235](https://github.com/netresearch/ofelia/issues/235).
- `console-width`: uint = `0`
  - Initial pseudo-TTY console size in columns. Same TTY-gating and Docker API floor as `console-height`. Either field can be set independently; the daemon uses its default for any dimension left at `0`.
- `environment`
  - Environment variables you want to set in the running container. **Note:** only supported in Docker API v1.30 and above
  - Same format as used with `-e` flag within `docker run`. For example: `FOO=bar`
    - **INI config**: `Environment` setting can be provided multiple times for multiple environment variables.
    - **Labels config**: multiple environment variables has to be provided as JSON array: `["FOO=bar", "BAZ=qux"]`
- `working-dir`: string
  - Working directory for the command execution, similar to `docker exec --workdir <dir>`
  - **Backward compatibility:** Requires Docker API v1.35+ (Docker Engine 17.09+). On older Docker versions, this parameter is silently ignored and the exec runs in the container's default working directory
  - If not specified, uses the working directory defined in the container image
- `privileged`: boolean = `false`
  - Run the exec in privileged mode, similar to `docker exec --privileged`
- `no-overlap`: boolean = `false`
  - Prevent that the job runs concurrently
- `history-limit`: integer = `10`
  - Number of past executions kept in memory
- `run-on-startup`: boolean = `false`
  - Run the job once immediately when the scheduler starts, before regular cron-based scheduling begins. Startup executions are dispatched in non-blocking goroutines so they do not delay scheduler startup.

### INI-file example

```ini
[job-exec "flush-nginx-logs"]
schedule = @hourly
container = nginx-proxy
command = /bin/bash /flush-logs.sh
user = www-data
tty = false

[job-exec "backup-logs"]
schedule = @daily
container = web-app
command = tar czf /backups/logs.tar.gz .
working-dir = /var/log
```

### Docker labels example

```sh
docker run -it --rm \
    --label ofelia.enabled=true \
    --label ofelia.job-exec.flush-nginx-logs.schedule="@hourly" \
    --label ofelia.job-exec.flush-nginx-logs.command="/bin/bash /flush-logs.sh" \
    --label ofelia.job-exec.flush-nginx-logs.user="www-data" \
    --label ofelia.job-exec.flush-nginx-logs.tty="false" \
        nginx

docker run -it --rm \
    --label ofelia.enabled=true \
    --label ofelia.job-exec.backup-logs.schedule="@daily" \
    --label ofelia.job-exec.backup-logs.command="tar czf /backups/logs.tar.gz ." \
    --label ofelia.job-exec.backup-logs.working-dir="/var/log" \
        web-app
```

When specifying exec jobs via labels, Ofelia prefixes the job name to keep jobs from different containers with the same label from clashing. By default the prefix is the Docker Compose **service** name (falling back to the container name), so the example above yields `nginx.flush-nginx-logs`. This default is collision-prone when one daemon watches several Compose projects that reuse a service name; the `[global]` `job-exec-label-scope` option (`service` / `container` / `container-service`) selects a collision-safe scheme. See [Cross-Container Job References](CONFIGURATION.md#cross-container-job-references-docker-compose) for details.

### Cancellation

The Docker Engine API cannot stop an exec'd process, so canceling an execution
does not stop the command; enforce a limit inside it, e.g. `timeout 30s ...`.
After a cancellation Ofelia inspects the exec and records what it finds: a
command that exited meanwhile is classified by its exit code; one that is still
running fails the execution with `exec still running` and the exec ID; if the
inspection fails, the execution fails with `exec outcome unknown`.

## `run`

This job can be used in 2 situations:

1. To run a command inside of a new container, using a specific image, similar to `docker run`
1. To start a stopped container, similar to `docker start`

### Parameters

- **`schedule`: string** (1, 2)
  - When the job should be executed. E.g. every 10 seconds or every night at 1 AM.
- `command`: string = default container command (1)
  - Command you want to run inside the container.
- **`image`: string** (1)
  - Image you want to use for the job.
  - If left blank, Ofelia assumes you will specify a container to start (situation 2).
- `entrypoint`: string (1)
  - Override the image entrypoint. Use an empty value to remove it.
- `user`: string = `nobody` (1)
  - User as which the command should be executed, similar to `docker run --user <user>`
  - If not set, inherits the global `default-user` (default `nobody`) — an empty value here means "inherit", not "container default". Set `user = default` for the container's own default user regardless of the global; `default` is reserved for that meaning and cannot name a real user
- `network`: string (1)
  - Connect the container to this network
- `hostname`: string (1)
  - Define the hostname of the instantiated container, e.g. `test-server`
- `container-name`: string (1)
  - Name assigned to the created container. Defaults to the job name. If left
    empty, Docker will choose a random name.
- `delete`: boolean = `true` (1)
  - Delete the container after the job is finished. Similar to `docker run --rm`
- `stop-signal`: string = `""` (image's `STOPSIGNAL`, falling back to `SIGTERM`) (1, 2)
  - Signal sent to the main process when Ofelia stops the container after execution. Accepts the canonical name (`SIGINT`, `SIGUSR1`) or the bare suffix (`INT`, `USR1`). Useful for apps with signal handlers (Node.js → `SIGINT`, Java thread-dump → `SIGQUIT`, custom cleanup → `SIGUSR1`/`SIGUSR2`). Pair with `stop-timeout` to give the application enough time to flush state before `SIGKILL`. Requires Docker API v1.42+ (Docker Engine 20.10+). See [#234](https://github.com/netresearch/ofelia/issues/234).
- `stop-timeout`: duration = `0s` (preserves the legacy 10s default in `cleanupOnDeadline`) (1, 2)
  - Grace period between the stop signal (see `stop-signal`) and the Docker daemon's escalation to `SIGKILL`. Honored only by Ofelia's deadline-cleanup path today (when a job exceeds its execution deadline); other shutdown paths inherit the daemon's default. Accepts a Go duration string (`30s`, `2m`, `1m30s`). Zero leaves the previous 10s default in effect so unconfigured jobs see no behavior change. See [#234](https://github.com/netresearch/ofelia/issues/234).
- **`container`: string** (2)
  - Name of the container you want to start.
  - Required field in case parameter `image` is not specified, no default.
- `tty`: boolean = `false` (1, 2)
  - Allocate a pseudo-tty, similar to `docker exec -t`. See this [Stack Overflow answer](https://stackoverflow.com/questions/30137135/confused-about-docker-t-option-to-allocate-a-pseudo-tty) for more info.
- `volume`:
  - Mount host machine directory into container as a [bind mount](https://docs.docker.com/storage/bind-mounts/#start-a-container-with-a-bind-mount)
  - Same format as used with `-v` flag within `docker run`. For example: `/tmp/test:/tmp/test:ro`
    - **INI config**: `Volume` setting can be provided multiple times for multiple mounts.
    - **Labels config**: multiple mounts has to be provided as JSON array: `["/test/tmp:/test/tmp:ro", "/test/tmp:/test/tmp:rw"]`
- `environment`
  - Environment variables you want to set in the running container.
  - Same format as used with `-e` flag within `docker run`. For example: `FOO=bar`
    - **INI config**: `Environment` setting can be provided multiple times for multiple environment variables.
    - **Labels config**: multiple environment variables has to be provided as JSON array: `["FOO=bar", "BAZ=qux"]`
- `working-dir`: string (1)
  - Working directory inside the container, similar to `docker run --workdir <dir>`
  - If not specified, uses the working directory defined in the container image
- `annotations`
  - Container annotations for metadata tracking, audit trails, and observability. Unlike labels, annotations don't affect Docker behavior.
  - Format: `key=value` strings. For example: `team=platform`, `cost-center=12345`
    - **INI config**: `Annotations` setting can be provided multiple times for multiple annotations.
    - **Labels config**: multiple annotations must be provided as JSON array: `["team=platform", "env=prod"]`
  - **Auto-populated annotations**: Ofelia automatically adds metadata to all containers:
    - `ofelia.job.name` - The job name
    - `ofelia.job.type` - Job type (run/service)
    - `ofelia.execution.time` - Execution timestamp (RFC3339)
    - `ofelia.scheduler.host` - Hostname running Ofelia
    - `ofelia.version` - Ofelia version
  - User annotations take precedence over auto-populated ones
  - **API requirement**: Docker API 1.43+ (Docker Engine 20.10.9+). On older versions, annotations are silently ignored.
- `no-overlap`: boolean = `false`
  - Prevent that the job runs concurrently
- `history-limit`: integer = `10`
  - Number of past executions kept in memory
- `max-runtime`: duration = `24h`
  - Maximum time the container is allowed to run before it is killed
- `run-on-startup`: boolean = `false`
  - Run the job once immediately when the scheduler starts, before regular cron-based scheduling begins. Startup executions are dispatched in non-blocking goroutines so they do not delay scheduler startup.

### INI-file example

```ini
[job-run "print-write-date"]
schedule = @every 5s
image = alpine:latest
command = sh -c 'date | tee -a /tmp/test/date'
volume = /tmp/test:/tmp/test:rw
environment = FOO=bar
environment = BAZ=qux

[job-run "backup-database"]
schedule = @daily
image = postgres:15
command = pg_dump mydb
annotations = team=platform
annotations = project=core-infra
annotations = environment=production
annotations = cost-center=12345
```

Then you can check output in host machine file `/tmp/test/date`

### Running Ofelia in Docker

```sh
docker run -it --rm \
    -v /var/run/docker.sock:/var/run/docker.sock:ro \
    --label ofelia.enabled=true \
    --label ofelia.job-run.print-write-date.schedule="@every 5s" \
    --label ofelia.job-run.print-write-date.image="alpine:latest" \
    --label ofelia.job-run.print-write-date.volume="/tmp/test:/tmp/test:rw" \
    --label ofelia.job-run.print-write-date.environment="FOO=bar" \
    --label ofelia.job-run.print-write-date.command="sh -c 'date | tee -a /tmp/test/date'" \
        ghcr.io/netresearch/ofelia:latest daemon

# Example with annotations for tracking and observability
docker run -it --rm \
    -v /var/run/docker.sock:/var/run/docker.sock:ro \
    --label ofelia.enabled=true \
    --label ofelia.job-run.backup.schedule="@daily" \
    --label ofelia.job-run.backup.image="postgres:15" \
    --label ofelia.job-run.backup.command="pg_dump mydb" \
    --label ofelia.job-run.backup.annotations='["team=platform", "project=core-infra", "env=prod"]' \
        ghcr.io/netresearch/ofelia:latest daemon
```

## `local`

Runs the command on the host running Ofelia.

**Note**: In case Ofelia is running inside a container, the command is executed inside the container. Not on the Docker host.

### Parameters

- **`schedule`: string**
  - When the job should be executed. E.g. every 10 seconds or every night at 1 AM.
- **`command`: string**
  - Command you want to run on the host.
- `dir`: string = `$(pwd)`
  - Base directory to execute the command.
- `environment`
  - Environment variables you want to set for the executed command.
  - Same format as used with `-e` flag within `docker run`. For example: `FOO=bar`
    - **INI config**: `Environment` setting can be provided multiple times for multiple environment variables.
    - **Labels config**: multiple environment variables has to be provided as JSON array: `["FOO=bar", "BAZ=qux"]`
- `no-overlap`: boolean = `false`
  - Prevent that the job runs concurrently
- `history-limit`: integer = `10`
  - Number of past executions kept in memory
- `run-on-startup`: boolean = `false`
  - Run the job once immediately when the scheduler starts, before regular cron-based scheduling begins. Startup executions are dispatched in non-blocking goroutines so they do not delay scheduler startup.

### INI-file example

```ini
[job-local "create-file"]
schedule = @every 15s
command = touch test.txt
dir = /tmp/
```

### Cancellation

When an execution is canceled — on scheduler shutdown, when the job is removed,
or at Ofelia's fixed 24-hour execution limit — Ofelia sends `SIGTERM` to the
process group the command runs in. Processes the command started belong to that
group. Whatever is still running after 10 seconds gets `SIGKILL`, and the
execution fails with `max runtime exceeded`. On Windows, which has no process
groups, only the command's own process is killed, at once. If the processes are
still there 10 seconds after the kill — for example because a process changed
its user and Ofelia may no longer signal it — the execution fails with
`local job termination unconfirmed` instead of waiting for the command to end.

## `service-run`

This job can be used to:

- To run a command inside a new `run-once` service, for running inside a swarm.

### Parameters

- **`schedule`: string** (1, 2)
  - When the job should be executed. E.g. every 10 seconds or every night at 1 AM.
- `command`: string = default container command (1, 2)
  - Command you want to run inside the container.
- **`image`: string** (1)
  - Image you want to use for the job.
  - If left blank, Ofelia assumes you will specify a container to start (situation 2).
- `network`: string (1)
  - Connect the container to this network
- `delete`: boolean = `true` (1)
  - Delete the container after the job is finished.
- `user`: string = `nobody` (1, 2)
  - User as which the command should be executed.
  - If not set, inherits the global `default-user` (default `nobody`) — an empty value here means "inherit", not "container default". Set `user = default` for the container's own default user regardless of the global; `default` is reserved for that meaning and cannot name a real user
- `tty`: boolean = `false` (1, 2)
  - Allocate a pseudo-tty, similar to `docker exec -t`. See this [Stack Overflow answer](https://stackoverflow.com/questions/30137135/confused-about-docker-t-option-to-allocate-a-pseudo-tty) for more info.
- `environment`
  - Environment variables passed to the service container.
  - Same format as used with `-e` flag within `docker run`. For example: `FOO=bar`
    - **INI config**: `Environment` setting can be provided multiple times for multiple environment variables.
    - **Labels config**: multiple environment variables must be provided as JSON array: `["FOO=bar", "BAZ=qux"]`
- `hostname`: string
  - Hostname for the service container.
- `dir`: string
  - Working directory inside the service container.
- `volume`
  - Mount host directories or named volumes into the service container.
  - Format: `source:target[:ro|rw]`. For example: `/host/path:/container/path:ro` or `myvolume:/data`
    - **INI config**: `Volume` setting can be provided multiple times for multiple mounts.
    - **Labels config**: multiple mounts must be provided as JSON array: `["/host:/container:ro", "data:/data"]`
- `annotations`
  - Service annotations for metadata tracking and observability. Stored as service labels in Docker Swarm.
  - Format: `key=value` strings. For example: `team=platform`, `environment=staging`
    - **INI config**: `Annotations` setting can be provided multiple times for multiple annotations.
    - **Labels config**: multiple annotations must be provided as JSON array: `["team=platform", "env=staging"]`
  - **Auto-populated annotations**: Ofelia automatically adds metadata:
    - `ofelia.job.name` - The job name
    - `ofelia.job.type` - Always "service" for service-run jobs
    - `ofelia.execution.time` - Execution timestamp (RFC3339)
    - `ofelia.scheduler.host` - Hostname running Ofelia
    - `ofelia.version` - Ofelia version
  - User annotations take precedence over auto-populated ones
- `no-overlap`: boolean = `false`
  - Prevent that the job runs concurrently
- `history-limit`: integer = `10`
  - Number of past executions kept in memory
- `max-runtime`: duration = `24h`
  - Maximum time the service task may run before it is removed
- `run-on-startup`: boolean = `false`
  - Run the job once immediately when the scheduler starts, before regular cron-based scheduling begins. Startup executions are dispatched in non-blocking goroutines so they do not delay scheduler startup.

### INI-file example

```ini
[job-service-run "service-executed-on-new-container"]
schedule = 0,20,40 * * * *
image = ubuntu
network = swarm_network
command =  touch /tmp/example

[job-service-run "swarm-backup"]
schedule = @daily
image = postgres:15
network = swarm_network
command = pg_dump mydb
environment = PGPASSWORD=secret
environment = PGHOST=db.internal
hostname = backup-worker
dir = /var/backups
volume = /backups:/backups:rw
annotations = team=data-platform
annotations = environment=staging
annotations = service-tier=backend
```

## `compose`

This job triggers commands via Docker Compose. Set `exec = true` to run commands
in an existing service container using `docker compose exec`. By default, a new
container is started with `docker compose run --rm`.

### Parameters

- **`schedule`: string**
  - When the job should be executed. E.g. every 10 seconds or every night at 1 AM.
- **`file`: string**
  - Path to the compose file, defaults to `compose.yml`.
- **`service`: string**
  - Service name to run or exec.
- `exec`: boolean = `false`
  - Use `docker compose exec` instead of `run`.
- `command`: string
  - Command passed to the service (optional).
- `max-runtime`: duration = `[global] max-runtime`
  - Maximum time a `run`-mode execution may run before its container is stopped
    and removed. Without a value of its own the job uses `[global] max-runtime`
    (default `24h`). Not supported with `exec = true`, because an exec'd
    command cannot be stopped: the value is ignored with a logged warning, and
    the job API rejects it.
- `run-on-startup`: boolean = `false`
  - Run the job once immediately when the scheduler starts, before regular cron-based scheduling begins. Startup executions are dispatched in non-blocking goroutines so they do not delay scheduler startup.

### INI-file example

```ini
[job-compose "backup"]
schedule = @daily
file = docker-compose.yml
service = db
command = pg_dumpall -U postgres
```

### Image for compose jobs

The default image contains no docker CLI, so `job-compose` fails in it. Each
release also publishes a compose variant of the image, with the docker CLI and
the Compose plugin from the image's Alpine release:

| Tag | Points to |
|---|---|
| `X.Y.Z-compose` | that release |
| `X.Y-compose`, `X-compose` | the latest release of that line |
| `latest-compose` | the latest release |

A prerelease gets only its `X.Y.Z-rc.N-compose` tag. The variant is otherwise
the default image: same entrypoint, configuration and `ofelia.*` labels; only
the title and description labels differ. It is built from the release's
binaries after their checksums were verified against the release's signed
`checksums.txt`.

The compose CLI runs inside the Ofelia container, but the Docker daemon
creates the containers. Paths in the compose file, such as relative bind
mounts, resolve inside the Ofelia container and the daemon then reads them as
host paths. Mount the project directory at the same path it has on the host:

```yaml
services:
  ofelia:
    image: ghcr.io/netresearch/ofelia:latest-compose
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./ofelia.ini:/etc/ofelia/config.ini:ro
      - /srv/myapp:/srv/myapp:ro   # same path as on the host
```

For a registered `[compose-project]` that uses file-backed Compose configs or
secrets, also provide writable private scratch storage at the **same absolute
path on the Docker host and inside Ofelia**. Compose bind-mounts their captured
files into the workload, so a container-private `/tmp` is not sufficient:

```yaml
services:
  ofelia:
    environment:
      TMPDIR: /var/lib/ofelia/compose-snapshots
    volumes:
      - /var/lib/ofelia/compose-snapshots:/var/lib/ofelia/compose-snapshots
```

Create that persistent host directory with mode `0700` and ownership matching
the Ofelia process. Do not share it with unrelated containers. The same requirement
applies with a remote Docker daemon: the scratch files must be visible there
at the same absolute path. Without file-backed configs or secrets, the private
model file is read only by the Compose CLI and a normal local temporary
directory is sufficient.

```ini
[job-compose "backup"]
schedule = @daily
file = /srv/myapp/compose.yml
service = db
command = pg_dumpall -U postgres
```

### Cancellation and maximum runtime

In the default `run` mode, Ofelia starts `docker compose run --rm -T` and labels
the run container with `ofelia.execution=<execution ID>`. When the execution is
canceled or reaches its `max-runtime` (the job's own value, else
`[global] max-runtime`), Ofelia stops the containers that
carry this label (stop signal, 10 seconds grace, then `SIGKILL`), ends the
compose CLI, removes the labeled containers and checks that none remain.
Dependency services that Compose started for the run do not carry the label and
keep running. If the Docker API cannot confirm that the run container is gone,
the execution fails with `compose run termination unconfirmed`.

The compose CLI uses the same Docker endpoint as Ofelia: `DOCKER_HOST` if set,
otherwise the default socket. `DOCKER_CONTEXT` is not passed to the CLI in `run`
mode, because Ofelia's own Docker client does not read Docker contexts and
could not find the run container on another daemon. When `DOCKER_HOST` is
unset and a Docker context other than `default` is selected — through
`DOCKER_CONTEXT` or `currentContext` in the CLI's `config.json` — each `run`
execution logs a warning that names the ignored context. Set `DOCKER_HOST` to
the daemon the jobs should run on.

In `exec` mode the command runs inside an existing container, and the Docker
Engine API cannot stop an exec'd process from outside. Canceling the execution
does not stop the command, and `max-runtime` has no effect in this mode; enforce
a limit inside the command, e.g. `timeout 30s ...`.

## Compose projects

A `[compose-project]` section registers a Compose project. Ofelia loads it with
[compose-go](https://github.com/compose-spec/compose-go), the reference
implementation of the Compose specification, and schedules every job in its
top-level `jobs:` section that has a `triggers.schedule` entry. The workload
stays defined in the Compose files: image, command, environment, mounts and
networks are not repeated in the Ofelia configuration. The design is recorded
in [ADR-004](adr/ADR-004-compose-jobs-as-job-source.md).

Compose projects need the Compose CLI 5.6.0 or later, the first release that
understands top-level `jobs:`; the [compose image variant](#image-for-compose-jobs)
contains it. Ofelia runs `docker compose version` when it loads the
configuration. When the CLI is missing or older, it logs an error that names
the version it found and schedules no job of any Compose project; all other
jobs run as usual. After a CLI upgrade, the next configuration poll loads the
projects.

### Parameters

- **`file`: string, repeatable**
  - A Compose file. Repeat the key for several files; they are merged in the
    order given, as with `docker compose -f a.yaml -f b.yaml`.
- `profile`: string, repeatable
  - A profile to activate. Jobs behind other profiles are not scheduled. Without
    this key, `COMPOSE_PROFILES` from Ofelia's environment or the env files
    applies, as for `docker compose`.
- `env-file`: string, repeatable
  - An env file for interpolation, replacing the default `.env` of the project
    directory.
- `project-name`: string
  - Overrides the project name Compose would derive, including one set by
    `COMPOSE_PROJECT_NAME`.
- `project-directory`: string
  - Overrides the project's working directory, which is where the default
    `.env` is read from.

The project is loaded as `docker compose` loads it, with the same order of
environment, env files, profiles and name. Each run passes the resolved project
name, profiles and directory explicitly, so it uses the context the job was
loaded with.

The section is read from the INI file only; container labels cannot register a
project.

The jobs of a project are bounded by `[global] max-runtime`, like a
`[job-compose]` without a value of its own.

### Jobs and names

Each scheduled job becomes an Ofelia job named `<registration>.<job>`, for
example `shop.backup`. It runs through the same adapter as `job-compose`, so it
needs the docker CLI and the Compose plugin (see
[Image for compose jobs](#image-for-compose-jobs)), and it is canceled the same
way (see [Cancellation and maximum runtime](#cancellation-and-maximum-runtime)).
The registered job runs with a private accepted model and interpolation environment:

```text
docker compose -f <accepted-model> --all-resources --profile <profile>... --env-file <accepted-env> \
  -p <project> --project-directory <dir> run --rm -T -l ofelia.execution=<id> <job>
```

Jobs without a schedule are not scheduled by Ofelia; they stay manual-only and
can be run with `docker compose run <job>`. When a job name is already used by
a `[job-compose]` section, the project is rejected with a logged error and the
`[job-compose]` keeps its name. `ofelia validate` exits with an error when any
project is rejected. It runs the same Compose CLI check as the daemon, so run it
where the Compose CLI 5.6.0 or later is installed, for example in the `-compose`
image variant.

Different registration names must not schedule the same job in the same
resolved Compose project. Every registration uses Ofelia's Docker endpoint,
so the pair of the resolved project name and Compose job name identifies a
workload. If two registrations claim that pair, both projects are rejected
as a whole; choosing different files, profiles or registration aliases does
not create a separate workload identity. Distinct projects or distinct jobs
within one project may be registered separately.

On every configuration poll (`config-poll-interval`), Ofelia loads each
registered project again and reloads the configuration when the resolved model
differs. That covers the files the section names as well as the default `.env`,
files pulled in through `include:` or `extends`, and interpolated values. A
project that loaded before and is rejected by a reload, for example after a
broken edit, keeps its last loaded jobs and logs the error; the rest of the
reload is applied, and the project's jobs are replaced once it loads again.
Retained jobs undergo the same identity check. Last loaded definitions stay
scheduled only while their identities remain distinct; a collision between a
retained definition and a newly loaded project rejects both registrations.
Each job carries a revision, a hash of its own definition and of every job and
service in its `depends_on` closure and their referenced Compose resources,
including captured config and secret contents, which `docker compose run` starts with it:
only jobs whose revision changed are replaced, and each execution logs the
revision it ran with. A running execution keeps the definition it started with.

Registered projects run the accepted, resolved Compose model from a private
temporary file created for each execution.
Includes, overrides, interpolation, relative paths, and job/service environment
and label files are resolved when the project loads. File-backed and
environment-backed Compose configs and secrets are captured at that point.
Optional dependencies behind inactive profiles do not run and do not require
their environment or configuration files until their profile is enabled.
File-backed inputs retain their original file mode and Unix UID/GID; Ofelia
fails the execution explicitly if its runtime cannot reproduce that ownership.
An unprivileged Ofelia process can preserve files it owns but needs suitable
privileges to reproduce another owner's UID/GID. Environment-backed inputs
keep Compose's native upload behavior and per-reference access settings.
A rejected reload therefore continues
running the last accepted workload, even if its original files have changed
or are no longer readable. Resolved values are kept in memory and are excluded
from API output and the schedule state file. The private model and interpolation
environment are removed after each execution.

Captured file inputs are retained in `TMPDIR/ofelia-compose-inputs` with stable
paths derived from their contents and access settings. They remain there after
the CLI and Ofelia exit, because dependency service containers can still bind
them and must be able to restart. The parent directory is private (`0700`);
mounted file permissions preserve the accepted source. Reuse checks the bytes
and access settings and rejects symlinks or altered files. Ofelia does not
automatically prune this directory. Remove its contents only after all containers
that reference those paths have been removed; retain it across host restarts.

Ofelia reserves the dependency-service label `ofelia.compose.resources` for a
fingerprint of that service's accepted config and secret inputs, including
pre-start hooks. This lets Compose recreate an existing dependency when those
inputs change on a valid reload. Unchanged or unrelated dependency services keep
their containers; changing a job's command or schedule does not force recreation.

This snapshot covers the Compose definition and these configuration inputs.
Image tags, build contexts, bind-mounted application data and external Docker
resources keep their normal runtime behavior; their contents are not pinned.
Use immutable image digests and versioned build inputs where reproducible
application contents are required. A plain `[job-compose]` continues reading
its configured file for each run.

### Supported triggers

Ofelia accepts a subset of the Compose trigger model and rejects everything
else when it loads the project. The logged error names the job and the
setting, and the project schedules none of its jobs, because a setting mapped
silently to a different behavior would be a compatibility bug. Other projects
and all other jobs are unaffected, and the project is loaded again once its
files change.

| Setting | Supported | Behavior |
|---|---|---|
| `cron` with five fields | yes | Evaluated by Ofelia's scheduler |
| `cron` with other field counts or `@daily` style shortcuts | no | Rejected |
| `timezone` | yes | The schedule is evaluated in that zone; an empty value means the local time zone of the Ofelia process, which is logged when the project is loaded. A daylight-saving switch does not count as a change of the job |
| `concurrency: forbid` (also the default) | yes | A fire while a run is in progress is skipped and logged |
| `concurrency: queue` | yes, with `--state-file` | A fire while a run is in progress waits in one queue slot and runs when that run ends; a further fire while the slot is full is dropped and logged |
| `missed_fires: one`, or unset | yes, with `--state-file` | After a downtime, one catch-up run for the most recent missed fire |
| `missed_fires: skip` | yes | Missed runs are not replayed |
| more than one schedule entry | no | Rejected |
| `triggers.manual: false` on the job or on a job it depends on | no | Rejected, because Ofelia runs the job with `docker compose run`, which refuses such jobs |

The plain form `schedule: ["0 2 * * *"]` sets only the cron expression, so
`missed_fires` stays at its default `one` and the job needs a state file. Without
`--state-file`, write the structured form with `missed_fires: skip`. Compose
merges lists across files by appending, so a schedule entry in a second file
adds a second entry; use `!override` to replace the list.

### Missed fires and the queue

`missed_fires: one` and `concurrency: queue` need state that survives a restart,
so Ofelia keeps a schedule record per job in the state file
([`--state-file`](CONFIGURATION.md#state-file---state-file), see
[ADR-005](adr/ADR-005-durable-schedule-state.md)). Without a state file, a
project that uses either setting is rejected with an error that names the flag.
`ofelia validate` applies the same check; pass it `--state-file`, or set
`OFELIA_STATE_FILE`, as for the daemon.

The record holds the most recent scheduled time the running scheduler
evaluated, and the run it admitted last. From it:

- **Catch-up.** A fire is missed only when Ofelia was not running at its time.
  At startup, Ofelia runs one catch-up per job, for the most recent missed fire,
  however long the downtime was. A job seen for the first time catches up
  nothing; fires before that are never missed. A fire the scheduler skipped
  while it was running (`concurrency: forbid`) is not missed either.
- **Changed schedule.** A changed `cron`, `timezone`, `concurrency` or
  `missed_fires` starts the record afresh: a time the old schedule produced is
  not a missed fire of the new one, and a queued run is dropped and logged.
  A change to the job's definition alone keeps the record.
- **Disabled job.** A disabled job catches up nothing. Disabling drops a queued
  run; enabling counts the fires of the disabled period as handled.
- **Manual runs.** `POST /api/jobs/run` and the web UI are refused with
  `409 Conflict` while a run of the job is in progress or waiting, so a manual
  run never runs beside a scheduled one. A manual run does not count as a fire.
- **Global limit.** For these jobs the global limit of concurrent jobs delays a
  run instead of dropping it. While the run waits for a slot, the job counts as
  busy.
- **At most once.** Ofelia records a run before it starts it. After a crash, a
  run that had started counts as handled with the outcome `unknown`, and is not
  started again; check it by hand. While its container (found by the
  `ofelia.execution` label) still runs, the job counts as busy. In exec mode
  (`docker compose exec`) Ofelia cannot see the command, so after a crash a
  second run can start beside one that still runs.

Each fire of such a job writes the state file: two atomic writes per run (start
and end), a third when the run waits for a slot, and one for a fire that is
skipped or queued.

An older Ofelia refuses to load a state file that holds schedule records (it
rejects fields it does not know). Before downgrading, remove the `schedules`
section from the file.

### Keep scheduled jobs out of `docker compose up`

Docker Compose 5.6 refuses `docker compose up` when an active job has a
schedule, because Docker Engine does not run scheduled jobs yet. Put scheduled
jobs behind a profile that `docker compose up` does not activate, and list that
profile in the `[compose-project]` section:

```yaml
# /srv/shop/compose.yaml
name: shop
services:
  web:
    image: registry.example.com/shop:2026.10
jobs:
  backup:
    image: registry.example.com/shop-maintenance:2026.10
    command: ["/app/bin/backup"]
    profiles: ["scheduled-jobs"]
    triggers:
      schedule:
        - cron: "0 2 * * *"
          timezone: Europe/Berlin
          concurrency: forbid
          missed_fires: skip
```

```ini
[compose-project "shop"]
file = /srv/shop/compose.yaml
profile = scheduled-jobs
```

This schedules `shop.backup` at 02:00 Berlin time. `docker compose up` in
`/srv/shop` starts `web` and leaves the job alone.
