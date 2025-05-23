# Ofelia - a job scheduler [![GitHub version](https://badge.fury.io/gh/netresearch%2Fofelia.svg)](https://github.com/netresearch/ofelia/releases) [![go test](https://github.com/netresearch/ofelia/actions/workflows/test.yml/badge.svg)](https://github.com/netresearch/ofelia/actions/workflows/test.yml)

<img src="https://weirdspace.dk/FranciscoIbanez/Graphics/Ofelia.gif" align="right" width="180px" height="300px" vspace="20" />

**Ofelia** orchestrates container tasks with minimal overhead, offering a sleek alternative to cron.

Label your Docker containers and let this Go-powered daemon handle the schedule.

## Table of Contents

- [Features](#features)
- [Using it](#using-it)
- [Configuration](#configuration)
- [Development](#development)
- [License](#license)

## Features

- **Job types** for running commands in running containers, new containers,
  on the host or as one-off swarm services.
- **Logging middlewares** integrate with systems like Slack or StatsD to report
  job output and status.
- **Dynamic Docker detection** polls containers at an interval controlled by
  `--docker-poll-interval` or listens for events with `--docker-events`. The same
  interval also controls automatic reloads of `ofelia.ini`.
- **Config validation** via the `validate` command to check your configuration
  before running.
- **Optional pprof server** enabled with `--enable-pprof` and bound via
  `--pprof-address` for profiling and debugging.
- **Optional web UI** enabled with `--enable-web` and bound via
  `--web-address` to view job status.

This fork is based off of [mcuadros/ofelia](https://github.com/mcuadros/ofelia).

## Using it

### Docker

The easiest way to deploy **Ofelia** is using a container runtime like **Docker**.

    docker pull ghcr.io/netresearch/ofelia

The image exposes a Docker health check so you can use `depends_on.condition: service_healthy` in Docker Compose.

### Standalone

If you don't want to run **Ofelia** using our (Docker) container image, you can download a binary from [our releases page](https://github.com/netresearch/ofelia/releases).

    wget https://github.com/netresearch/ofelia/releases/latest

Alternatively, you can build Ofelia from source:

```sh
make packages  # build packages under ./bin
# or
go build .
```


### Commands

Use `ofelia daemon` to run the scheduler with a configuration file and
`ofelia validate` to check the configuration without starting the daemon:

```sh
ofelia daemon --config=/path/to/config.ini
ofelia validate --config=/path/to/config.ini
```

When `--enable-pprof` is specified, the daemon starts a Go pprof HTTP
server for profiling. Use `--pprof-address` to set the listening address
(default `127.0.0.1:8080`).
When `--enable-web` is specified, the daemon serves a small web UI at
`--web-address` (default `:8081`) to inspect job status.

## Configuration

### Jobs

#### Scheduling format

This application uses the [Go implementation of `cron`](https://pkg.go.dev/github.com/robfig/cron) with a parser for supporting optional seconds.

Supported formats:

- `@every 10s`
- `20 0 1 * * *` (every night, 20 seconds after 1 AM - [Quartz format](http://www.quartz-scheduler.org/documentation/quartz-2.3.0/tutorials/tutorial-lesson-06.html)
- `0 1 * * *` (every night at 1 AM - standard [cron format](https://en.wikipedia.org/wiki/Cron)).

You can configure four different kinds of jobs:

- `job-exec`: this job is executed inside of a running container.
- `job-run`: runs a command inside of a new container, using a specific image.
- `job-local`: runs the command inside of the host running ofelia.
- `job-service-run`: runs the command inside a new "run-once" service, for running inside a swarm

See [Jobs reference documentation](docs/jobs.md) for all available parameters.
See [Architecture overview](docs/architecture.md) for details about the scheduler, job types and middleware.

### Logging

**Ofelia** comes with three different logging drivers that can be configured in the `[global]` section or as top-level Docker labels:

- `mail` to send mails
- `save` to save structured execution reports to a directory
- `slack` to send messages via a slack webhook

### Global Options

- `smtp-host` - address of the SMTP server.
- `smtp-port` - port number of the SMTP server.
- `smtp-user` - user name used to connect to the SMTP server.
- `smtp-password` - password used to connect to the SMTP server.
- `smtp-tls-skip-verify` - when `true` ignores certificate signed by unknown authority error.
- `email-to` - mail address of the receiver of the mail.
- `email-from` - mail address of the sender of the mail.
- `mail-only-on-error` - only send a mail if the execution was not successful.

- `save-folder` - directory in which the reports shall be written.
- `save-only-on-error` - only save a report if the execution was not successful.

- `slack-webhook` - URL of the slack webhook.
- `slack-only-on-error` - only send a slack message if the execution was not successful.
- `log-level` - logging level (DEBUG, INFO, NOTICE, WARNING, ERROR, CRITICAL). When set in the config file this level is applied from startup unless `--log-level` is provided.

### INI-style configuration

Run with `ofelia daemon --config=/path/to/config.ini`

```ini
[global]
save-folder = /var/log/ofelia_reports
save-only-on-error = true
log-level = INFO

[job-exec "job-executed-on-running-container"]
schedule = @hourly
container = my-container
command = touch /tmp/example

[job-run "job-executed-on-new-container"]
schedule = @hourly
image = ubuntu:latest
command = touch /tmp/example

[job-local "job-executed-on-current-host"]
schedule = @hourly
command = touch /tmp/example

[job-service-run "service-executed-on-new-container"]
schedule = 0,20,40 * * * *
image = ubuntu
network = swarm_network
command =  touch /tmp/example
```

### Docker label configurations

In order to use this type of configuration, Ofelia needs access to the Docker socket.

> ⚠ **Warning**: This command changed! Please remove the `--docker` flag from your command.

```sh
docker run -it --rm \
    -v /var/run/docker.sock:/var/run/docker.sock:ro \
    --label ofelia.save-folder="/var/log/ofelia_reports" \
    --label ofelia.save-only-on-error="true" \
    --label ofelia.log-level="INFO" \
        ghcr.io/netresearch/ofelia:latest daemon
```

Labels format: `ofelia.<JOB_TYPE>.<JOB_NAME>.<JOB_PARAMETER>=<PARAMETER_VALUE>`.
This type of configuration supports all the capabilities provided by INI files, including the global logging options.
For `job-exec` labels, Ofelia automatically prefixes the container name to the job name to avoid collisions. A label `ofelia.job-exec.optimize` on a container named `gitlab` will result in a job called `gitlab.optimize`.

Also, it is possible to configure `job-exec` by setting labels configurations on the target container. To do that, additional label `ofelia.enabled=true` need to be present on the target container.

For example, we want `ofelia` to execute `uname -a` command in the existing container called `nginx`.
To do that, we need to start the `nginx` container with the following configurations:

```sh
docker run -it --rm \
    --label ofelia.enabled=true \
    --label ofelia.job-exec.test-exec-job.schedule="@every 5s" \
    --label ofelia.job-exec.test-exec-job.command="uname -a" \
        nginx
```

### Example Compose setup

See the [example](example/) directory for a ready-made `compose.yml` that
demonstrates the different job types. It starts an `nginx` container with an
`exec` job label and configures additional `run`, `local` and `service-run` jobs
via `ofelia.ini`.

The Docker image expects a configuration file at `/etc/ofelia/config.ini` and
runs `daemon --config /etc/ofelia/config.ini` by default. Mount your file at
that location so no `command:` override is required:

```yaml
services:
  ofelia:
    image: ghcr.io/netresearch/ofelia:latest
    volumes:
      - ./ofelia.ini:/etc/ofelia/config.ini:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
```

If you choose a different path, update both the volume mount and the `--config`
flag.

**Ofelia** reads labels of all Docker containers for configuration by default. To apply on a subset of containers only, use the flag `--docker-filter` (or `-f`) similar to the [filtering for `docker ps`](https://docs.docker.com/engine/reference/commandline/ps/#filter). E.g. to apply only to the current Docker Compose project using a `label` filter:

You can also configure how often Ofelia polls Docker for label changes and reloads
the INI configuration. The default interval is `10s`. Override it with
`--docker-poll-interval` or the `poll-interval` option in the `[docker]` section
of the config file. Set it to `0` to disable both polling and automatic reloads.

Because the Docker image defines an `ENTRYPOINT`, pass the scheduler
arguments as a list in `command:` so Compose does not treat them as a single
string.

```yaml
version: "3"
services:
  ofelia:
    image: ghcr.io/netresearch/ofelia:latest
    depends_on:
      - nginx
    command: ["daemon", "-f", "label=com.docker.compose.project=${COMPOSE_PROJECT_NAME}"]
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    labels:
      ofelia.job-local.my-test-job.schedule: "@every 5s"
      ofelia.job-local.my-test-job.command: "date"
  nginx:
    image: nginx
    labels:
      ofelia.enabled: "true"
      ofelia.job-exec.datecron.schedule: "@every 5s"
      ofelia.job-exec.datecron.command: "uname -a"
```

Ofelia polls Docker every 10 seconds to detect label changes and reload the INI
file. The interval can be adjusted using `--docker-poll-interval`. Event-based
updates can be enabled with `--docker-events`; when enabled, polling can be
disabled entirely with `--docker-no-poll`. Setting the interval to `0` also
disables both label polling and INI reloads. Polling can also be disabled in
`ofelia.ini` by adding `no-poll = true` under the `[docker]` section:

```ini
[docker]
no-poll = true
```

### Dynamic Docker configuration

You can start Ofelia in its own container or on the host itself, and it will dynamically pick up any container that starts, stops or is modified on the fly.
In order to achieve this, you simply have to use Docker containers with the labels described above and let Ofelia take care of the rest.

### Hybrid configuration (INI files + Docker)

You can specify part of the configuration on the INI files, such as globals for the middlewares or even declare tasks in there but also merge them with Docker.
The Docker labels will be parsed, added and removed on the fly but the config file can also be used.

Use the INI file to:

- Configure any middleware
- Configure any global setting
- Create a `run` jobs, so they executes in a new container each time

```ini
[global]
slack-webhook = https://myhook.com/auth

[job-run "job-executed-on-new-container"]
schedule = @hourly
image = ubuntu:latest
command = touch /tmp/example
```

Use docker to:

- Create `exec` jobs

```sh
docker run -it --rm \
    --label ofelia.enabled=true \
    --label ofelia.job-exec.test-exec-job.schedule="@every 5s" \
    --label ofelia.job-exec.test-exec-job.command="uname -a" \
        nginx
```

## Development

### Linting

The CI workflow runs `go vet` and checks code formatting with `gofmt`. Run these checks locally with:

```sh
go vet ./...
gofmt -l $(git ls-files '*.go')
```

The pipeline fails if any file is not properly formatted or if `go vet` reports issues.

### Testing

See [running tests](docs/tests.md) for Docker requirements and how to run `go test`.

## License

This project is released under the [MIT License](LICENSE).
