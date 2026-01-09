# Ofelia - a job scheduler

[![Go Reference](https://pkg.go.dev/badge/github.com/netresearch/ofelia.svg)](https://pkg.go.dev/github.com/netresearch/ofelia)
[![CI](https://github.com/netresearch/ofelia/actions/workflows/ci.yml/badge.svg)](https://github.com/netresearch/ofelia/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/netresearch/ofelia/graph/badge.svg)](https://codecov.io/gh/netresearch/ofelia)
[![CodeQL](https://github.com/netresearch/ofelia/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/netresearch/ofelia/actions/workflows/github-code-scanning/codeql)
[![GitHub release](https://img.shields.io/github/v/release/netresearch/ofelia)](https://github.com/netresearch/ofelia/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/netresearch/ofelia)](go.mod)
[![License](https://img.shields.io/github/license/netresearch/ofelia)](LICENSE)
[![Container Image](https://img.shields.io/badge/ghcr.io-netresearch%2Fofelia-blue)](https://github.com/netresearch/ofelia/pkgs/container/ofelia)
[![Go Report Card](https://goreportcard.com/badge/github.com/netresearch/ofelia)](https://goreportcard.com/report/github.com/netresearch/ofelia)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/11513/badge)](https://www.bestpractices.dev/projects/11513)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/netresearch/ofelia/badge)](https://securityscorecards.dev/viewer/?uri=github.com/netresearch/ofelia)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)
[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](https://slsa.dev)

<img src="https://weirdspace.dk/FranciscoIbanez/Graphics/Ofelia.gif" align="right" width="180px" height="300px" vspace="20" />

**Ofelia** orchestrates container tasks with minimal overhead, offering a sleek alternative to cron.

Label your Docker containers and let this Go-powered daemon handle the schedule.

## Table of Contents

- [Requirements](#requirements)
- [Features](#features)
- [Using it](#using-it)
- [Environment variables](#environment-variables)
- [Configuration precedence](#configuration-precedence)
- [Configuration](#configuration)
- [Development](#development)
- [Documentation](#documentation)
- [Roadmap](#roadmap)
- [License](#license)

## Requirements

### For Docker-based jobs (job-exec, job-run, job-service-run)

**Docker Engine**: Version 17.06 or later (API 1.30+)
- Ofelia requires Docker API 1.30 because it uses the `Env` parameter for exec operations
- The Docker client automatically negotiates the API version with your daemon
- Tested with Docker Engine 29.0+ (API 1.44+)

**Docker socket access**:
- Mount `/var/run/docker.sock` into the Ofelia container (read-only recommended)
- Or set `DOCKER_HOST` environment variable to your Docker daemon

### For Compose jobs (job-compose)

**Docker Compose**: Version 2.0 or later
- Required only if using `job-compose` type
- Must be available in the container's PATH or host system

### For local jobs only (job-local)

No Docker required - runs commands directly on the host system.

### Optional environment variables

- `DOCKER_API_VERSION`: Force a specific Docker API version (not recommended, auto-negotiation works)
- `DOCKER_HOST`: Docker daemon socket/host (default: `unix:///var/run/docker.sock`)
- `DOCKER_CERT_PATH`: Path to Docker TLS certificates
- `DOCKER_TLS_VERIFY`: Enable TLS verification

## Features

- **Job types** for running commands in running containers, new containers,
  on the host or as one-off swarm services.
- **Webhook notifications** send job execution results to Slack, Discord, Teams, Matrix, ntfy, Pushover, PagerDuty, Gotify, or custom endpoints with preset-based configuration and SSRF protection.
- **Logging middlewares** integrate with email, file saves, and legacy Slack to report
  job output and status.
- **Dynamic Docker detection** polls containers at an interval controlled by
  `--docker-poll-interval` or listens for events with `--docker-events`. The same
  interval also controls automatic reloads of `ofelia.ini` when the file changes.
- **Config validation** via the `validate` command to check your configuration
  before running.
- **Optional pprof server** enabled with `--enable-pprof` and bound via
  `--pprof-address` for profiling and debugging.
- **Optional web UI** enabled with `--enable-web` and bound via
  `--web-address` to view job status. Static files for the UI are embedded in
  the binary.
- **Removed job history** keeps deregistered jobs in memory and shows them in the web UI.
- **Enhanced web UI** allows editing and deleting jobs, shows job origin and type, displays each job's configuration and renders the scheduler configuration in a table with empty job sections hidden. Action buttons now use clear icons.
- **Timezone selector** in the web UI lets you view timestamps in local, server or UTC time and remembers your choice.
- **Job dependencies** allow defining execution order with `depends-on`, and conditional triggers with `on-success` and `on-failure` to create job workflows.

> For detailed feature documentation, see [Configuration Reference](docs/CONFIGURATION.md) and [Job Types](docs/jobs.md).

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
`ofelia validate` to check the configuration without starting the daemon. The
`validate` command prints the complete configuration including applied defaults:

```sh
ofelia daemon --config=/path/to/config.ini
ofelia validate --config=/path/to/config.ini
```

The `--config` flag also supports glob patterns so multiple INI files can be
combined:

```sh
ofelia daemon --config=/etc/ofelia/conf.d/*.ini
```

If `--config` is omitted, Ofelia looks for `/etc/ofelia/config.ini`.

When `--enable-pprof` is specified, the daemon starts a Go pprof HTTP
server for profiling. Use `--pprof-address` to set the listening address
(default `127.0.0.1:8080`).
When `--enable-web` is specified, the daemon serves a small web UI at
`--web-address` (default `:8081`). Besides inspecting running and removed jobs,
the UI allows starting jobs manually, disabling or enabling them and creating,
updating or deleting jobs of all types. A second table lists jobs removed from
the configuration via `/api/jobs/removed`. The endpoint `/api/jobs/{name}/history`
exposes past runs including stdout, stderr and any error messages while
`/api/config` returns the active configuration as JSON. The UI includes a
timezone selector so you can view times in your local zone, the server zone or
UTC and have that preference saved locally.
Creating `run` or `exec` jobs requires Ofelia to run with Docker access; the
server rejects such requests if no Docker client is available.

#### Interactive Setup

Use `ofelia init` to create a configuration file interactively:

```sh
ofelia init
```

The wizard guides you through creating jobs step-by-step, prompting for:
- Job type (local, run, exec, service-run)
- Job name and schedule (cron expression)
- Command to execute
- Container name (for Docker-based jobs)
- Optional settings like working directory, user, and environment variables

By default, the configuration is saved to `./ofelia.ini`. Use `--output` (or `-o`)
to specify a different location:

```sh
ofelia init --output=/etc/ofelia/config.ini
```

#### Health Diagnostics

Use `ofelia doctor` to check your Ofelia setup and diagnose common issues:

```sh
ofelia doctor
ofelia doctor --config=/path/to/config.ini
```

The doctor command performs comprehensive health checks:
- **Configuration**: Validates config file syntax and job definitions
- **Docker**: Tests Docker daemon connectivity and permissions
- **Jobs**: Checks for schedule conflicts, invalid cron expressions, and
  container references

If no `--config` is specified, doctor searches these locations (in order):
`./ofelia.ini`, `./config.ini`, `/etc/ofelia/config.ini`, `/etc/ofelia.ini`.

Use `--json` for machine-readable output suitable for monitoring integrations.

### Environment variables

You can configure the same options with environment variables. When set,
they override values from the config file and Docker labels.

| Variable | Corresponding flag | Description |
| --- | --- | --- |
| `OFELIA_CONFIG` | `--config` | Path or glob pattern to the configuration file(s) |
| `OFELIA_DOCKER_FILTER` | `--docker-filter` | Docker container filter (comma separated for multiple) |
| `OFELIA_POLL_INTERVAL` | `--docker-poll-interval` | Interval for Docker polling and config reload |
| `OFELIA_DOCKER_EVENTS` | `--docker-events` | Use Docker events instead of polling |
| `OFELIA_DOCKER_NO_POLL` | `--docker-no-poll` | Disable polling Docker for labels |
| `OFELIA_LOG_LEVEL` | `--log-level` | Set the log level |
| `OFELIA_ENABLE_PPROF` | `--enable-pprof` | Enable the pprof HTTP server |
| `OFELIA_PPROF_ADDRESS` | `--pprof-address` | Address for the pprof server |
| `OFELIA_ENABLE_WEB` | `--enable-web` | Enable the web UI |
| `OFELIA_WEB_ADDRESS` | `--web-address` | Address for the web UI server |

> See [Configuration Reference](docs/CONFIGURATION.md) for all available options including job-specific parameters.

### Configuration precedence

Ofelia merges options from multiple sources in the following order. Values from later sources override earlier ones:

1. Built-in defaults
2. `config.ini`
3. Docker labels
4. Command-line flags
5. Environment variables

The daemon watches `config.ini` and reloads it automatically when the file changes.

Job definitions and most `[global]` middleware options (`slack-*`, `save-*`,
`mail-*`, `log-level`, `max-runtime`) are applied on reload. Options that start
servers (`enable-web`, `web-address`, `enable-pprof`, `pprof-address`) and all
`[docker]` settings require restarting the daemon.

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
- `job-compose`: runs a command using `docker compose run` or `docker compose exec` based on a compose file

See [Jobs reference documentation](docs/jobs.md) for all available parameters.
See [Architecture overview](docs/architecture.md) for details about the scheduler, job types and middleware.

### Logging

**Ofelia** comes with several logging/notification drivers:

- `webhook` to send notifications via Slack, Discord, Teams, ntfy, Pushover, PagerDuty, Gotify, or custom HTTP endpoints. See [Webhook Documentation](docs/webhooks.md) for configuration details.
- `mail` to send mails
- `save` to save structured execution reports to a directory. The destination folder is created automatically if it doesn't exist.
- `slack` (**deprecated**) to send messages via a slack webhook - migrate to the new webhook system

### Global Options

- `smtp-host` - address of the SMTP server.
- `smtp-port` - port number of the SMTP server.
- `smtp-user` - user name used to connect to the SMTP server.
- `smtp-password` - password used to connect to the SMTP server.
- `smtp-tls-skip-verify` - when `true` ignores certificate signed by unknown authority error.
- `email-to` - mail address of the receiver of the mail.
- `email-from` - mail address of the sender of the mail.
- `email-subject` - custom subject template for emails. Uses Go template syntax with access to `.Job` and `.Execution`. Example: `[ALERT] Job {{.Job.GetName}} {{status .Execution}}`. If not set, uses default format.
- `mail-only-on-error` - only send a mail if the execution was not successful.

- `save-folder` - directory in which the reports shall be written. The folder is created automatically if it doesn't exist using an equivalent of `mkdir -p`.
- `save-only-on-error` - only save a report if the execution was not successful.

- `webhook-allow-remote-presets` - allow fetching presets from remote URLs (default: `false`).
- `webhook-preset-cache-ttl` - cache duration for remote presets (default: `24h`).

- `slack-webhook` - (**deprecated**) URL of the slack webhook. Migrate to `[webhook "name"]` sections.
- `slack-only-on-error` - (**deprecated**) only send a slack message if the execution was not successful.
- `log-level` - logging level (DEBUG, INFO, NOTICE, WARNING, ERROR, CRITICAL). When set in the config file this level is applied from startup unless `--log-level` is provided.
- `enable-web` - enable the built-in web UI.
- `web-address` - address for the web UI server (default `:8081`).
- `enable-pprof` - enable the pprof debug server.
- `pprof-address` - address for the pprof server (default `127.0.0.1:8080`).
- `max-runtime` - default maximum duration a run or service job may run (default `24h`).

Log output now includes the original file and line of the logging call instead of the adapter location.

### INI-style configuration

Run with `ofelia daemon --config=/path/to/config.ini` or use a glob pattern like
`/etc/ofelia/conf.d/*.ini` to load multiple files

```ini
[global]
save-folder = /var/log/ofelia_reports
save-only-on-error = true
log-level = INFO
enable-web = true
web-address = :8081
enable-pprof = true
pprof-address = 127.0.0.1:8080

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
    --label ofelia.enable-web="true" \
    --label ofelia.web-address=":8081" \
    --label ofelia.enable-pprof="true" \
    --label ofelia.pprof-address="127.0.0.1:8080" \
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
`exec` job label and configures additional `run`, `local`, `service-run` and
`compose` jobs via `ofelia.ini`.

Compose jobs help address feature requests such as
[#359](https://github.com/mcuadros/ofelia/issues/359),
[#358](https://github.com/mcuadros/ofelia/issues/358),
[#333](https://github.com/mcuadros/ofelia/issues/333),
[#318](https://github.com/mcuadros/ofelia/issues/318),
[#290](https://github.com/mcuadros/ofelia/issues/290) and
[#247](https://github.com/mcuadros/ofelia/issues/247).

The Docker image expects a configuration file at `/etc/ofelia/config.ini` and
runs `daemon --config /etc/ofelia/config.ini` by default. You can also mount a
directory and use a glob pattern such as `/etc/ofelia/conf.d/*.ini`. Mount your
file at the chosen location so no `command:` override is required:

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
the INI configuration when the file has changed. The default interval is `10s`.
Override it with `--docker-poll-interval` or the `poll-interval` option in the
`[docker]` section of the config file. Set it to `0` to disable both polling and
automatic reloads. Command-line values only override the configuration when the
flags are explicitly provided.

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

### Container Detection and Configuration Reloading

Ofelia separates two concerns:

1. **Container detection**: Detecting when Docker containers start/stop to pick up label changes
2. **Config file watching**: Reloading the INI file when it changes

#### Default Behavior (Recommended)

By default, Ofelia uses **Docker events** for instant container detection and **polls** the config file every 10 seconds:

| Setting | Default | Purpose |
|---------|---------|---------|
| `events` | `true` | Real-time container detection via Docker events |
| `config-poll-interval` | `10s` | How often to check for INI file changes |
| `docker-poll-interval` | `0` | Container polling (disabled, events used instead) |
| `polling-fallback` | `10s` | Auto-enable container polling if events fail |

#### Configuration Options

```ini
[docker]
# INI file reload interval (set to 0 to disable config watching)
config-poll-interval = 10s

# Container detection via Docker events (recommended)
events = true

# Explicit container polling interval (0 = disabled)
# WARNING: Running both events and polling is usually wasteful
docker-poll-interval = 0

# Auto-fallback to polling if event subscription fails (BC-safe default)
# Set to 0 to disable fallback (will only log errors)
polling-fallback = 10s
```

#### CLI Flags

- `--docker-config-poll-interval`: INI file reload interval
- `--docker-events`: Enable/disable Docker event-based container detection
- `--docker-poll-interval`: Container polling interval (fallback)
- `--docker-polling-fallback`: Auto-fallback interval if events fail

#### Backwards Compatibility

The old `poll-interval` and `no-poll` options still work but are deprecated:

```ini
[docker]
# DEPRECATED: Use config-poll-interval and docker-poll-interval instead
poll-interval = 10s

# DEPRECATED: Use docker-poll-interval=0 instead
no-poll = true
```

### Dynamic Docker configuration

You can start Ofelia in its own container or on the host itself, and it will dynamically pick up any container that starts, stops or is modified on the fly.
In order to achieve this, you simply have to use Docker containers with the labels described above and let Ofelia take care of the rest.

### Hybrid configuration (INI files + Docker)

You can specify part of the configuration on the INI files, such as globals for the middlewares or even declare tasks in there but also merge them with Docker.
The Docker labels will be parsed, added and removed on the fly but the config file can also be used. Run and exec jobs defined in the INI file remain active even when no labeled containers are found. Jobs detected via Docker labels are managed separately and can disappear when the corresponding container is removed.

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

### Getting Started

Set up your development environment with automated git hooks:

```sh
make setup
```

This installs [lefthook](https://github.com/evilmartians/lefthook) (Go-native git hooks) and configures all quality checks to run automatically before each commit.

### Quality Checks

The project enforces code quality through automated hooks and CI:

```sh
make help         # Show all available commands
make dev-check    # Run all quality checks
make lint         # Run golangci-lint
make test         # Run tests
```

**Automated git hooks** (via lefthook):

- **Pre-commit** (~4-6s): Quality gates (go mod tidy, go vet, gofmt, golangci-lint, gosec, secret detection)
- **Commit-msg**: Message format validation (conventional commits recommended)
- **Pre-push** (~10-30s): Full test suite with race detection + protected branch warnings
- **Post-checkout**: Dependency change reminders
- **Post-merge**: Auto-update dependencies

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for detailed development guide.

### Testing

See [running tests](docs/tests.md) for Docker requirements and how to run `go test`.

## Documentation

Detailed documentation is available in the [`docs/`](docs/) directory:

| Document | Description |
|----------|-------------|
| [Configuration Reference](docs/CONFIGURATION.md) | Complete guide to all configuration options, job parameters, and middleware settings |
| [Job Types](docs/jobs.md) | Detailed documentation for each job type (exec, run, local, service-run, compose) |
| [Webhook Notifications](docs/webhooks.md) | Configure notifications via Slack, Discord, Teams, Matrix, ntfy, Pushover, PagerDuty, Gotify |
| [Architecture Overview](docs/architecture.md) | System design, scheduler internals, and component interactions |
| [Security Guide](docs/SECURITY.md) | Security best practices, vulnerability reporting, and hardening recommendations |
| [API Reference](docs/API.md) | Web UI API endpoints and OpenAPI specification |
| [Troubleshooting](docs/TROUBLESHOOTING.md) | Common issues and solutions |
| [Development Guide](docs/DEVELOPMENT.md) | Contributing, testing, and local development setup |
| [Quick Reference](docs/QUICK_REFERENCE.md) | Cheat sheet for common configurations |

## Roadmap

This project uses GitHub Issues as a living roadmap:

- **[Feature Requests](https://github.com/netresearch/ofelia/issues?q=is%3Aissue+is%3Aopen+label%3Aenhancement)** - Planned enhancements and new features
- **[Bug Reports](https://github.com/netresearch/ofelia/issues?q=is%3Aissue+is%3Aopen+label%3Abug)** - Known issues being addressed
- **[Good First Issues](https://github.com/netresearch/ofelia/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22)** - Great starting points for contributors

### Current Focus Areas

1. **Container orchestration** - Enhanced Docker and Compose integration
2. **Observability** - Improved logging, metrics, and monitoring
3. **Configuration** - More flexible job configuration options
4. **Web UI** - Continued improvements to the management interface

Want to influence the roadmap? [Open an issue](https://github.com/netresearch/ofelia/issues/new) or contribute a PR!

## License

This project is released under the [MIT License](LICENSE).
