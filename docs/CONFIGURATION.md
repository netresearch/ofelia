# Ofelia Configuration Guide

## Configuration Methods

Ofelia supports multiple configuration methods that can be used independently or combined:

1. **INI Configuration File** (Traditional, static configuration)
2. **Docker Labels** (Dynamic, container-specific configuration)
3. **Environment Variables** (Override specific settings)
4. **Command-line Flags** (Runtime overrides)

## Configuration Precedence

Configuration sources are evaluated in the following order (highest to lowest priority):

1. Command-line flags
2. Environment variables
3. INI configuration file
4. Docker labels

### Hybrid Configuration (INI + Docker Labels)

A common pattern is using INI configuration for global settings (like email credentials) while using Docker labels for job definitions. This keeps sensitive credentials out of container metadata and allows dynamic job discovery.

**Example Setup**:

1. **Create INI config with global settings only** (`/etc/ofelia/config.ini`):

```ini
[global]
# Email notification settings (credentials hidden from labels)
smtp-host = smtp.gmail.com
smtp-port = 587
smtp-user = notifications@example.com
smtp-password = ${SMTP_PASSWORD}
email-from = notifications@example.com
email-to = admin@example.com

# Slack notifications
slack-webhook = https://hooks.slack.com/services/XXX/YYY/ZZZ
slack-only-on-error = true

# Output settings
save-folder = /var/log/ofelia
save-only-on-error = true

# No job definitions in INI - jobs come from Docker labels
```

2. **Docker Compose with config volume and labels** (`docker-compose.yml`):

```yaml
version: '3.8'
services:
  ofelia:
    image: netresearch/ofelia:latest
    command: daemon --config=/etc/ofelia/config.ini
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./config.ini:/etc/ofelia/config.ini:ro
      - ./logs:/var/log/ofelia
    environment:
      - SMTP_PASSWORD=${SMTP_PASSWORD}  # Injected from .env file

  database:
    image: postgres:15
    labels:
      ofelia.enabled: "true"
      # Jobs inherit email settings from INI [global] section
      ofelia.job-exec.backup.schedule: "@daily"
      ofelia.job-exec.backup.command: "pg_dump -U postgres mydb > /backup/db.sql"
      ofelia.job-exec.backup.email-to: "dba@example.com"  # Override recipient
```

**How It Works**:
- Ofelia always reads the config file first (defaults to `/etc/ofelia/config.ini`)
- Global settings (email, slack, save options) are loaded from INI
- Docker labels provide job definitions dynamically
- Jobs inherit global notification settings unless explicitly overridden
- If a job is defined in both INI and labels with the same name, the INI version takes precedence

**Benefits**:
- Credentials stay in config files, not exposed in `docker inspect`
- Jobs can be added/removed by updating container labels
- Global settings centralized in one place
- Environment variable substitution for secrets (`${SMTP_PASSWORD}`)

> [!NOTE]
> Please check the `Include stopped containers` documentaion below if you are using `--docker-include-stopped` flag.

### Labels-Only Configuration (No INI File)

Ofelia can run entirely without an INI configuration file, using only Docker labels and environment variables. This is ideal for simple setups, Kubernetes environments, or when you want all configuration in one place.

**When the INI file is missing or unreadable, Ofelia**:
- Logs a warning but continues running
- Creates an empty internal configuration
- Relies entirely on Docker labels for job definitions
- Uses environment variables for daemon settings

**Example: Pure Docker Labels Setup**

```yaml
version: '3.8'
services:
  ofelia:
    image: netresearch/ofelia:latest
    command: daemon --docker-events
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      # Daemon settings via environment variables
      - OFELIA_LOG_LEVEL=info
      - OFELIA_ENABLE_WEB=true
      - OFELIA_WEB_ADDRESS=:8081
    labels:
      # Mark this as the Ofelia service container
      ofelia.service: "true"
      ofelia.enabled: "true"
      # Global settings via labels (on Ofelia container with ofelia.service=true)
      ofelia.slack-webhook: "https://hooks.slack.com/services/XXX/YYY/ZZZ"
      ofelia.slack-only-on-error: "true"
      # job-run can be defined on Ofelia container
      ofelia.job-run.cleanup.schedule: "@daily"
      ofelia.job-run.cleanup.image: "alpine:latest"
      ofelia.job-run.cleanup.command: "echo 'Daily cleanup'"
      ofelia.job-run.cleanup.delete: "true"
    ports:
      - "8081:8081"

  app:
    image: myapp:latest
    labels:
      ofelia.enabled: "true"
      # job-exec defined on target container
      ofelia.job-exec.health.schedule: "*/5 * * * *"
      ofelia.job-exec.health.command: "curl -f http://localhost:8080/health"
```

**Key Points**:

| Setting Type | Configuration Method | Notes |
|--------------|---------------------|-------|
| Daemon options | Environment variables | `OFELIA_*` prefix |
| Global notifications | Labels on Ofelia container | Requires `ofelia.service=true` |
| job-exec | Labels on target container | Container auto-detected |
| job-run, job-local, job-service-run | Labels on Ofelia container | Requires `ofelia.service=true` |

**Available Environment Variables**:

| Variable | Description | Default |
|----------|-------------|---------|
| `OFELIA_CONFIG` | Config file path | `/etc/ofelia/config.ini` |
| `OFELIA_LOG_LEVEL` | Logging level (DEBUG, INFO, WARNING, ERROR) | INFO |
| `OFELIA_DOCKER_FILTER` | Docker container filter | (none) |
| `OFELIA_POLL_INTERVAL` | Deprecated legacy poll interval (affects config and container polling) | (unset) |
| `OFELIA_DOCKER_EVENTS` | Use Docker events instead of polling | true |
| `OFELIA_DOCKER_NO_POLL` | Disable Docker polling | false |
| `OFELIA_DOCKER_INCLUDE_STOPPED` | Include stopped containers when reading Docker labels (only for job-run) | false |
| `OFELIA_ENABLE_WEB` | Enable web UI | false |
| `OFELIA_WEB_ADDRESS` | Web UI bind address | :8081 |
| `OFELIA_WEB_AUTH_ENABLED` | Enable web UI authentication | false |
| `OFELIA_WEB_USERNAME` | Web UI username | (none) |
| `OFELIA_WEB_PASSWORD_HASH` | bcrypt hash of password | (none) |
| `OFELIA_WEB_SECRET_KEY` | Secret for token signing | (auto-generated) |
| `OFELIA_WEB_TOKEN_EXPIRY` | Token expiry in hours | 24 |
| `OFELIA_WEB_MAX_LOGIN_ATTEMPTS` | Max login attempts per minute | 5 |
| `OFELIA_ENABLE_PPROF` | Enable pprof profiling | false |
| `OFELIA_PPROF_ADDRESS` | pprof bind address | 127.0.0.1:8080 |

**Limitations of Labels-Only Configuration**:
- No environment variable substitution in label values (`${VAR}` won't expand)
- Sensitive values (passwords, API keys) visible in `docker inspect`
- Global notification settings require `ofelia.service=true` on Ofelia container
- Per-job SMTP credentials not recommended (use INI for credentials)

**When to Use Labels-Only**:
- Simple setups without email notifications
- Slack-only notifications (webhook URL is less sensitive)
- Development and testing environments
- When all configuration should be in docker-compose.yml

**When to Use Hybrid (INI + Labels)**:
- Production environments with email notifications
- When credentials must be protected
- When you need environment variable substitution for secrets

#### Include stopped containers (OFELIA_DOCKER_INCLUDE_STOPPED, --docker-include-stopped)

You can enable `include-stopped` via the env var **`OFELIA_DOCKER_INCLUDE_STOPPED`**, the flag **`--docker-include-stopped`**, or in config under `[docker]` as **`include-stopped = true`**. Default is `false`.

**Purpose**

- **Job-run on stopped containers:** This option is for running **job-run** jobs whose Ofelia labels are defined on **stopped** containers (e.g. scheduled backup, configure or migrate tasks on containters that stops after executing a task). Other job types (job-exec, job-local, job-service-run, job-compose) from stopped containers are ignored; only job-run labels are parsed on stopped containers.
- **Decentralization:** Each service can own its job-run definitions via Docker labels on its own container instead of configuring them only on the Ofelia service container.

**Behaviour**

- When `include-stopped` is true, Ofelia searches for job-run labels across **all** matching containers (running and stopped). Stopped containers are included in the label scan.
- Only one definition per job-run **name** is kept. Definitions from **running** containers take precedence over definitions from stopped containers.
- When two containers (both stopped) define the same job-run name, the definition from the container that is effectively processed last is used (e.g. last created or last in iteration). Avoid defining the same job-run name on multiple stopped containers if you need a predictable result.

**Recommendations**

- Set up **container pruning** (or a clear lifecycle for stopped containers) so that old stopped containers with Ofelia labels do not accumulate and are not used unintentionally.
- Prefer specifying a **Docker filter** (`--docker-filter` or `[docker]` `filters`) to limit which containers Ofelia inspects; this reduces the set of running and stopped containers considered.

## INI Configuration

### Basic Structure

```ini
[global]
# Global configuration options

[job-TYPE "NAME"]
# Job-specific configuration
# TYPE: exec, run, local, service, compose
# NAME: Unique job identifier
```

### Global Settings

```ini
[global]
# Docker Configuration
docker-host = unix:///var/run/docker.sock
docker-poll-interval = 30s
docker-events = true
allow-host-jobs-from-labels = false
default-user = nobody        # Default for exec/run/service; empty uses container default

# Notification Settings
slack-url = https://hooks.slack.com/services/XXX/YYY/ZZZ
slack-channel = #alerts
slack-only-on-error = true

# Email Settings
email-from = ofelia@example.com
email-to = admin@example.com
smtp-host = smtp.gmail.com
smtp-port = 587
smtp-user = ofelia@example.com
smtp-password = ${SMTP_PASSWORD}  # Environment variable reference

# Output Settings
save-folder = /var/log/ofelia
save-only-on-error = false

# Web UI Settings
enable-web = true
# Bind to localhost by default to avoid exposing the web UI to untrusted networks.
# Change only when you have proper network access controls in place.
web-address = 127.0.0.1:8080

# Web UI Authentication (RECOMMENDED for production)
# WARNING: Disabling auth exposes /api/* endpoints including job creation/execution.
# If auth is disabled, ensure web-address is bound to localhost or a protected interface.
web-auth-enabled = true
web-username = admin
web-password-hash = $2a$12$...  # bcrypt hash - use 'ofelia hash-password' to generate
web-secret-key = ${WEB_SECRET_KEY}  # Required for persistent sessions across restarts
web-token-expiry = 24  # hours
web-max-login-attempts = 5

# Monitoring
enable-pprof = false
pprof-address = :6060

# Security
enable-strict-validation = false
```

## Job Types

### ExecJob - Execute in Existing Container

Runs commands inside an already-running container.

```ini
[job-exec "database-backup"]
# Required
schedule = @midnight        # Cron expression or preset
container = postgres         # Container name or ID
command = pg_dump mydb > /backup/db.sql

# Optional
user = postgres             # User to run command as
environment = DB_NAME=mydb,BACKUP_RETENTION=7
tty = false                 # Allocate TTY
delay = 5s                  # Delay before execution

# Middleware Configuration
slack-webhook = https://hooks.slack.com/...
slack-channel = #db-alerts
slack-only-on-error = true

email-to = dba@example.com
email-subject = Database Backup Report

save-folder = /logs/backups
save-only-on-error = false

overlap = false             # Prevent overlapping runs
```

### RunJob - Execute in New Container

Creates a new container for each job execution.

```ini
[job-run "data-processor"]
# Required
schedule = 0 */6 * * *      # Every 6 hours
image = myapp/processor:latest

# Optional
command = process-data --mode=batch
pull = always               # always, never, if-not-present
network = backend           # Docker network
user = 1000:1000           # UID:GID or username
hostname = processor-job

# Container Configuration
environment = ENV=production,LOG_LEVEL=info
volumes = /data:/data:ro,/output:/output:rw
devices = /dev/fuse:/dev/fuse
capabilities-add = SYS_ADMIN
capabilities-drop = NET_RAW
dns = 8.8.8.8,8.8.4.4
labels = job=processor,env=prod
working-dir = /app
memory = 512m
memory-swap = 1g
cpu-shares = 512
cpu-quota = 50000

# Cleanup
delete = true               # Delete container after execution
delete-timeout = 30s        # Timeout for deletion

# Restart Policy
restart-on-failure = 3      # Max restart attempts
```

### LocalJob - Execute on Host

Runs commands directly on the host machine.

```ini
[job-local "system-cleanup"]
# Required
schedule = @daily
command = /usr/local/bin/cleanup.sh

# Optional
user = maintenance          # System user
dir = /var/maintenance      # Working directory
environment = CLEANUP_DAYS=30,LOG_FILE=/var/log/cleanup.log

# Security Warning: LocalJobs run with host privileges
# Not available from Docker labels unless explicitly allowed
```

### ServiceJob - Docker Swarm Service

Deploys as a Docker Swarm service (requires Swarm mode).

```ini
[job-service-run "distributed-task"]
# Inherits all RunJob configuration
schedule = @hourly
image = myapp/worker:latest
command = run-distributed-task

# Swarm-specific
replicas = 3                # Number of replicas
placement-constraints = node.role==worker
resources-limits-cpu = 2
resources-limits-memory = 1g
resources-reservations-cpu = 0.5
resources-reservations-memory = 256m
restart-policy = on-failure
restart-delay = 10s
restart-max-attempts = 3
```

### ComposeJob - Docker Compose Operations

Manages Docker Compose projects.

```ini
[job-compose "stack-restart"]
# Required
schedule = 0 4 * * *        # 4 AM daily
project = myapp             # Project name
command = restart           # Compose command

# Optional
service = web               # Specific service
timeout = 300s              # Operation timeout
dir = /opt/compose/myapp    # Working directory with docker-compose.yml
environment = COMPOSE_PROJECT_NAME=myapp
```

## Docker Labels Configuration

Configure jobs using container labels:

### Basic Label Format

```yaml
labels:
  # Enable Ofelia for this container
  ofelia.enabled: "true"
  
  # Job configuration: ofelia.JOB-TYPE.JOB-NAME.PROPERTY
  ofelia.job-exec.backup.schedule: "@midnight"
  ofelia.job-exec.backup.command: "backup.sh"
  ofelia.job-exec.backup.user: "root"
```

### Complete Example

```yaml
version: '3.8'
services:
  database:
    image: postgres:15
    labels:
      # Enable Ofelia
      ofelia.enabled: "true"
      
      # Backup job
      ofelia.job-exec.db-backup.schedule: "0 2 * * *"
      ofelia.job-exec.db-backup.command: "pg_dump -U postgres mydb > /backup/db.sql"
      ofelia.job-exec.db-backup.user: "postgres"
      ofelia.job-exec.db-backup.environment: "PGPASSWORD=secret"
      
      # Maintenance job
      ofelia.job-exec.db-vacuum.schedule: "@weekly"
      ofelia.job-exec.db-vacuum.command: "vacuumdb --all --analyze"
      
      # Health check job
      ofelia.job-exec.db-health.schedule: "@every 5m"
      ofelia.job-exec.db-health.command: "pg_isready -U postgres"
      ofelia.job-exec.db-health.slack-only-on-error: "true"
      
  app:
    image: myapp:latest
    labels:
      ofelia.enabled: "true"
      
      # Cache warming
      ofelia.job-exec.cache-warm.schedule: "0 */4 * * *"
      ofelia.job-exec.cache-warm.command: "php artisan cache:warm"
      
      # Queue processing
      ofelia.job-exec.queue-process.schedule: "@every 1m"
      ofelia.job-exec.queue-process.command: "php artisan queue:work --stop-when-empty"
```

### Global Settings in Docker Compose

Docker labels configure **jobs only**, not global settings like logging or output storage. For global configuration in Docker Compose, use environment variables on the Ofelia container:

```yaml
version: '3.8'
services:
  ofelia:
    image: netresearch/ofelia:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./logs:/var/log/ofelia  # Mount for save-folder
    environment:
      # Logging Configuration
      - OFELIA_LOG_LEVEL=debug            # DEBUG, INFO, WARNING, ERROR

      # Docker Settings
      - OFELIA_DOCKER_EVENTS=true         # Use events instead of polling
      - OFELIA_POLL_INTERVAL=30s          # Poll interval for labels
      - OFELIA_DOCKER_FILTER=label=monitored=true  # Filter containers

      # Web UI
      - OFELIA_ENABLE_WEB=true
      - OFELIA_WEB_ADDRESS=:8080

      # Performance Profiling
      - OFELIA_ENABLE_PPROF=false
    labels:
      ofelia.enabled: "true"
      # Job-run labels go on Ofelia container
      ofelia.job-run.cleanup.schedule: "@daily"
      ofelia.job-run.cleanup.image: "alpine:latest"
      ofelia.job-run.cleanup.command: "rm -rf /tmp/*"
```

**Configuration by Method**:

| Setting Type | Docker Labels | Environment Variables | INI Config |
|--------------|---------------|----------------------|------------|
| Job schedules | ✅ Yes | ❌ No | ✅ Yes |
| Job commands | ✅ Yes | ❌ No | ✅ Yes |
| Log level | ❌ No | ✅ `OFELIA_LOG_LEVEL` | ✅ `log-level` |
| Save folder | ❌ No | ❌ No | ✅ `save-folder` |
| Save only on error | ❌ No | ❌ No | ✅ `save-only-on-error` |
| Docker host | ❌ No | ❌ No | ✅ `docker-host` |
| Web UI | ❌ No | ✅ `OFELIA_ENABLE_WEB` | ✅ `enable-web` |

**Note**: For output capture (`save-folder`, `save-only-on-error`), use an INI configuration file. These settings require file system paths and are not available via environment variables or labels.

## Schedule Expressions

### Cron Format

Standard cron expressions with seconds (optional):

```
┌───────────── second (0-59) [OPTIONAL]
│ ┌───────────── minute (0-59)
│ │ ┌───────────── hour (0-23)
│ │ │ ┌───────────── day of month (1-31)
│ │ │ │ ┌───────────── month (1-12)
│ │ │ │ │ ┌───────────── day of week (0-7, 0 and 7 are Sunday)
│ │ │ │ │ │
│ │ │ │ │ │
* * * * * *
```

### Preset Schedules

```ini
@yearly     # Run once a year (0 0 1 1 *)
@annually   # Same as @yearly
@monthly    # Run once a month (0 0 1 * *)
@weekly     # Run once a week (0 0 * * 0)
@daily      # Run once a day (0 0 * * *)
@midnight   # Same as @daily
@hourly     # Run once an hour (0 * * * *)
@every 5m   # Run every 5 minutes
@every 1h30m # Run every 1.5 hours
@triggered  # Only run when triggered (via on-success, on-failure, or RunJob)
@manual     # Alias for @triggered
@none       # Alias for @triggered
```

### Examples

```ini
# Every 15 minutes
schedule = */15 * * * *

# Monday to Friday at 9 AM
schedule = 0 9 * * 1-5

# First day of month at 2:30 AM
schedule = 30 2 1 * *

# Every 30 seconds
schedule = */30 * * * * *

# Complex: Every 2 hours between 8 AM and 6 PM on weekdays
schedule = 0 8-18/2 * * 1-5
```

## Environment Variables

Override configuration using environment variables:

```bash
# Global settings
OFELIA_DOCKER_HOST=tcp://docker:2376
OFELIA_DOCKER_POLL_INTERVAL=1m
OFELIA_SLACK_URL=https://hooks.slack.com/...
OFELIA_JWT_SECRET=my-secret-key

# Job-specific (format: OFELIA_JOB_TYPE_NAME_PROPERTY)
OFELIA_JOB_EXEC_BACKUP_SCHEDULE=@hourly
OFELIA_JOB_RUN_CLEANUP_IMAGE=alpine:3.18
```

## Command-line Flags

```bash
ofelia daemon \
  --config=/etc/ofelia/config.ini \
  --docker-host=unix:///var/run/docker.sock \
  --docker-poll-interval=30s \
  --docker-events \
  --enable-web \
  --web-address=:8080 \
  --enable-pprof \
  --pprof-address=:6060 \
  --log-level=debug
```

## Middleware Configuration

### Webhook Notifications (Recommended)

The new webhook notification system supports multiple services (Slack, Discord, Teams, Matrix, ntfy, Pushover, PagerDuty, Gotify) with named webhooks that can be assigned to specific jobs.

```ini
[global]
webhook-allow-remote-presets = false

[webhook "slack-alerts"]
preset = slack
id = T00000000/B00000000000
secret = XXXXXXXXXXXXXXXXXXXXXXXX
trigger = error

[webhook "discord-notify"]
preset = discord
id = 1234567890123456789
secret = abcdefghijklmnopqrstuvwxyz1234567890ABCDEF
trigger = always

[job-exec "important-task"]
schedule = @daily
container = worker
command = important-task.sh
webhooks = slack-alerts, discord-notify
```

> **See [Webhook Documentation](./webhooks.md)** for complete configuration options, all bundled presets, custom preset creation, and security considerations.

### Slack Notifications (Deprecated)

> **Note**: The `slack-webhook` option is deprecated. Please migrate to the new [webhook notification system](#webhook-notifications-recommended) which provides better flexibility, multiple service support, and per-job webhook assignment.

```ini
[job-exec "important-task"]
schedule = @daily
container = worker
command = important-task.sh

# Slack settings (DEPRECATED - use [webhook "name"] sections instead)
slack-webhook = https://hooks.slack.com/services/XXX/YYY/ZZZ
slack-channel = #alerts
slack-only-on-error = false
slack-mentions = @channel
slack-icon-emoji = :robot:
slack-username = Ofelia Bot
```

### Email Notifications

```ini
[job-exec "critical-job"]
schedule = @hourly
container = app
command = critical-check.sh

# Email settings
email-to = ops@example.com,alerts@example.com
email-subject = Critical Job Report
email-from = ofelia@example.com
mail-only-on-error = true
```

### Configuration Inheritance

Notification settings support inheritance from global to job-level configuration. When a job specifies only some notification settings, the missing values are automatically inherited from the `[global]` section.

**Inheritance Rules:**

| Setting Type | Inherited Fields | Notes |
|--------------|------------------|-------|
| **Email** | `smtp-host`, `smtp-port`, `smtp-user`, `smtp-password`, `email-from`, `email-to`, `email-subject`, `mail-only-on-error` | SMTP connection details and behavior flags are inherited |
| **Slack** | `slack-webhook`, `slack-only-on-error` | Webhook URL and behavior flags are inherited |
| **Save** | `save-folder`, `save-only-on-error` | Save folder and behavior flags are inherited |

**Example: Partial Override**

```ini
[global]
# Define SMTP settings once
smtp-host = smtp.example.com
smtp-port = 587
smtp-user = notifications@example.com
smtp-password = ${SMTP_PASSWORD}
email-from = ofelia@example.com
email-to = ops@example.com

[job-exec "backup"]
schedule = @daily
container = postgres
command = pg_dump mydb > /backup/db.sql
# Only override error-only behavior - inherits all SMTP settings from global
mail-only-on-error = true

[job-exec "critical-check"]
schedule = @hourly
container = app
command = health-check.sh
# Override recipient - inherits SMTP settings from global
email-to = critical-alerts@example.com
```

**Important Notes:**

- Boolean fields (`mail-only-on-error`, `slack-only-on-error`, `save-only-on-error`) are fully inherited from global config and can be overridden per-job in both directions (global true + job false, or global false + job true)
- Job-level settings always take precedence over global settings when explicitly set
- To enable notifications for a job, at minimum specify `email-to` or `slack-webhook` at either global or job level

### Output Saving

```ini
[job-exec "data-export"]
schedule = @daily
container = exporter
command = export-data.sh

# Save output
save-folder = /var/log/ofelia/exports
save-only-on-error = false
save-format = json  # json or text
save-retention = 30d # Keep for 30 days
```

### Overlap Prevention

```ini
[job-exec "long-running"]
schedule = */10 * * * *
container = worker
command = long-task.sh

# Prevent overlapping runs
overlap = false
```

## Job Dependencies

Ofelia supports job dependencies to create workflows where jobs can depend on other jobs, or trigger other jobs on success or failure.

### Dependency Configuration

Define job execution order and conditional triggers:

```ini
[job-exec "init-database"]
schedule = @daily
container = postgres
command = /scripts/init-db.sh

[job-exec "backup-database"]
schedule = @daily
container = postgres
command = /scripts/backup.sh
# Wait for init-database to complete first
depends-on = init-database

[job-exec "process-data"]
schedule = @daily
container = worker
command = /scripts/process.sh
# Multiple dependencies (use multiple lines)
depends-on = init-database
depends-on = backup-database
# Trigger these jobs on success
on-success = notify-complete
# Trigger these jobs on failure
on-failure = alert-ops

[job-exec "notify-complete"]
schedule = @triggered
container = notifier
command = /scripts/success-notify.sh

[job-exec "alert-ops"]
schedule = @triggered
container = notifier
command = /scripts/failure-alert.sh
```

> **Note**: Jobs triggered only via `on-success` or `on-failure` should use `@triggered` (or aliases `@manual`/`@none`).
> These jobs are registered but not scheduled in cron - they only run when triggered by another job or manually.

### Dependency Options

| Option | Description | Example |
|--------|-------------|---------|
| `depends-on` | Jobs that must complete successfully before this job runs | `depends-on = init-job` |
| `on-success` | Jobs to trigger when this job completes successfully | `on-success = cleanup-job` |
| `on-failure` | Jobs to trigger when this job fails | `on-failure = alert-job` |

### Docker Labels Syntax

```yaml
version: '3.8'
services:
  worker:
    image: myapp:latest
    labels:
      ofelia.enabled: "true"

      # Main processing job
      ofelia.job-exec.process.schedule: "@hourly"
      ofelia.job-exec.process.command: "process.sh"
      ofelia.job-exec.process.depends-on: "setup"
      ofelia.job-exec.process.on-success: "cleanup"
      ofelia.job-exec.process.on-failure: "alert"

      # Setup job (dependency)
      ofelia.job-exec.setup.schedule: "@hourly"
      ofelia.job-exec.setup.command: "setup.sh"

      # Cleanup job (triggered on success)
      ofelia.job-exec.cleanup.schedule: "@triggered"
      ofelia.job-exec.cleanup.command: "cleanup.sh"

      # Alert job (triggered on failure)
      ofelia.job-exec.alert.schedule: "@triggered"
      ofelia.job-exec.alert.command: "alert.sh"
```

### Cross-Container Job References (Docker Compose)

When using Docker Compose, jobs are automatically named using the **service name** from `docker-compose.yml`, not the generated container name. This enables intuitive cross-container job references:

```yaml
version: '3.8'
services:
  database:
    image: postgres:15
    labels:
      ofelia.enabled: "true"
      ofelia.job-exec.backup.schedule: "@daily"
      ofelia.job-exec.backup.command: "pg_dump -U postgres mydb"
      ofelia.job-exec.backup.on-success: "app.notify"  # Reference job on 'app' service

  app:
    image: myapp:latest
    labels:
      ofelia.enabled: "true"
      ofelia.job-exec.process.schedule: "@hourly"
      ofelia.job-exec.process.command: "process.sh"
      ofelia.job-exec.process.depends-on: "database.backup"  # Wait for database backup
      ofelia.job-exec.notify.schedule: "@triggered"
      ofelia.job-exec.notify.command: "notify.sh"
```

Jobs are named as `{service}.{job-name}`:
- `database.backup` - Backup job on the database service
- `app.process` - Process job on the app service
- `app.notify` - Notify job on the app service

For non-Compose containers (without the `com.docker.compose.service` label), the container name is used instead.

### Important Notes

1. **Circular dependencies are detected** - Ofelia will reject configurations with circular dependency chains
2. **Dependencies must exist** - Referenced jobs must be defined in the configuration
3. **All job types supported** - Dependencies work across all job types (exec, run, local, service, compose)
4. **Multiple dependencies** - Use multiple `depends-on` lines in INI format to specify multiple dependencies
5. **Service name precedence** - Docker Compose service names take precedence over container names for job naming

## Security Considerations

### Restricting Host Jobs

```ini
[global]
# Prevent LocalJobs from Docker labels
allow-host-jobs-from-labels = false
```

### Web UI Authentication

Ofelia's web UI supports optional authentication to protect API endpoints:

```ini
[global]
# Enable authentication
web-auth-enabled = true
web-username = admin
web-password-hash = $2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/X4.F3V7Y8GdDmz7hG

# Token configuration
web-secret-key = ${WEB_SECRET_KEY}  # Auto-generated if not set
web-token-expiry = 24               # Hours
web-max-login-attempts = 5          # Per minute per IP
```

**Generating a password hash:**

```bash
# Using htpasswd (Apache utils)
htpasswd -bnBC 12 "" 'your-password' | tr -d ':\n'

# Using Python
python3 -c "import bcrypt; print(bcrypt.hashpw(b'your-password', bcrypt.gensalt(12)).decode())"
```

**Authentication flow:**
1. POST `/api/login` with `{"username":"...", "password":"..."}`
2. Receive token in response and `auth_token` cookie
3. Include token as `Authorization: Bearer <token>` or cookie for subsequent requests
4. POST `/api/logout` to invalidate token

**Security features:**
- bcrypt password hashing (cost 12)
- Rate limiting per IP
- CSRF token protection
- Secure cookie settings (HttpOnly, SameSite=Strict)
- Constant-time credential comparison

### Input Validation

Ofelia provides two levels of input validation:

**Basic Validation (Default)**
- Cron expression validation
- Required field checks
- Docker image name format validation

**Strict Validation (Opt-in)**

Enable strict validation for security-conscious environments:

```ini
[global]
enable-strict-validation = true
```

When enabled, strict validation provides:
- Command injection prevention (blocks shell metacharacters)
- Path traversal protection (blocks `../` patterns)
- Network restriction (blocks private IP ranges)
- File extension filtering (blocks `.sh`, `.exe`, etc.)
- Tool restrictions (blocks `wget`, `curl`, `rsync`, etc.)

**Default**: `false` (disabled)

**When to enable**:
- Multi-tenant environments with untrusted users
- Strict security compliance requirements (SOC2, PCI-DSS)
- Public-facing job scheduling systems
- Highly regulated environments

**When to keep disabled**:
- Infrastructure automation requiring shell scripts
- Backup operations using `rsync`, `wget`, `curl`
- Jobs accessing private networks (192.168.*, 10.*, 172.*)
- Airgapped/restricted environments with `.local` domains
- Most production deployments (Ofelia runs commands inside isolated containers)

## Best Practices

1. **Use environment variables for secrets**
   ```ini
   smtp-password = ${SMTP_PASSWORD}
   jwt-secret = ${JWT_SECRET}
   ```

2. **Enable Docker events for real-time updates**
   ```ini
   docker-events = true
   ```

3. **Set appropriate job timeouts**
   ```ini
   delete-timeout = 30s
   ```

4. **Use overlap prevention for long-running jobs**
   ```ini
   overlap = false
   ```

5. **Configure appropriate resource limits**
   ```ini
   memory = 512m
   cpu-shares = 512
   ```

6. **Use save-only-on-error for debugging**
   ```ini
   save-only-on-error = true
   ```

7. **Implement health checks**
   ```ini
   [job-exec "health-check"]
   schedule = @every 5m
   container = app
   command = health-check.sh
   slack-only-on-error = true
   ```

## Validation

Validate configuration before deployment:

```bash
# Validate INI file
ofelia validate --config=/etc/ofelia/config.ini

# Test specific job
ofelia test --config=/etc/ofelia/config.ini --job=backup

# Dry run (show what would be executed)
ofelia daemon --config=/etc/ofelia/config.ini --dry-run
```

---
*See also: [API Documentation](./API.md) | [CLI Package](./packages/cli.md) | [Project Index](./PROJECT_INDEX.md)*
