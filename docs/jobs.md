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
- `user`: string = `root`
  - User as which the command should be executed, similar to `docker exec --user <user>`
- `tty`: boolean = `false`
  - Allocate a pseudo-tty, similar to `docker exec -t`. See this [Stack Overflow answer](https://stackoverflow.com/questions/30137135/confused-about-docker-t-option-to-allocate-a-pseudo-tty) for more info.
- `environment`
  - Environment variables you want to set in the running container. **Note:** only supported in Docker API v1.30 and above
  - Same format as used with `-e` flag within `docker run`. For example: `FOO=bar`
    - **INI config**: `Environment` setting can be provided multiple times for multiple environment variables.
    - **Labels config**: multiple environment variables has to be provided as JSON array: `["FOO=bar", "BAZ=qux"]`
- `working-dir`: string
  - Working directory for the command execution, similar to `docker exec --workdir <dir>`
  - **Backward compatibility:** Requires Docker API v1.35+ (Docker Engine 17.09+). On older Docker versions, this parameter is silently ignored and the exec runs in the container's default working directory
  - If not specified, uses the working directory defined in the container image
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

When specifying exec jobs via labels, Ofelia adds the container name as a prefix to the job name. This prevents jobs from different containers with the same label from clashing. In the example above, the job will be called `nginx.flush-nginx-logs`.

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
- `user`: string = `root` (1)
  - User as which the command should be executed, similar to `docker run --user <user>`
- `network`: string (1)
  - Connect the container to this network
- `hostname`: string (1)
  - Define the hostname of the instantiated container, e.g. `test-server`
- `container-name`: string (1)
  - Name assigned to the created container. Defaults to the job name. If left
    empty, Docker will choose a random name.
- `delete`: boolean = `true` (1)
  - Delete the container after the job is finished. Similar to `docker run --rm`
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
        netresearch/ofelia:latest daemon

# Example with annotations for tracking and observability
docker run -it --rm \
    -v /var/run/docker.sock:/var/run/docker.sock:ro \
    --label ofelia.enabled=true \
    --label ofelia.job-run.backup.schedule="@daily" \
    --label ofelia.job-run.backup.image="postgres:15" \
    --label ofelia.job-run.backup.command="pg_dump mydb" \
    --label ofelia.job-run.backup.annotations='["team=platform", "project=core-infra", "env=prod"]' \
        netresearch/ofelia:latest daemon
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
- `user`: string = `root` (1, 2)
  - User as which the command should be executed.
- `tty`: boolean = `false` (1, 2)
  - Allocate a pseudo-tty, similar to `docker exec -t`. See this [Stack Overflow answer](https://stackoverflow.com/questions/30137135/confused-about-docker-t-option-to-allocate-a-pseudo-tty) for more info.
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
