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
web-address = :8080

# Monitoring
enable-pprof = false
pprof-address = :6060

# Security
jwt-secret = ${JWT_SECRET}
jwt-expiry-hours = 24
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
[job-service "distributed-task"]
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
@yearly    # Run once a year (0 0 1 1 *)
@annually  # Same as @yearly
@monthly   # Run once a month (0 0 1 * *)
@weekly    # Run once a week (0 0 * * 0)
@daily     # Run once a day (0 0 * * *)
@midnight  # Same as @daily
@hourly    # Run once an hour (0 * * * *)
@every 5m  # Run every 5 minutes
@every 1h30m # Run every 1.5 hours
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

### Slack Notifications

```ini
[job-exec "important-task"]
schedule = @daily
container = worker
command = important-task.sh

# Slack settings
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
email-only-on-error = true
```

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
schedule = @yearly
container = notifier
command = /scripts/success-notify.sh

[job-exec "alert-ops"]
schedule = @yearly
container = notifier
command = /scripts/failure-alert.sh
```

> **Note**: Jobs triggered only via `on-success` or `on-failure` still require a valid schedule.
> Use an infrequent schedule like `@yearly` to prevent scheduled runs while allowing triggered execution.

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
      ofelia.job-exec.cleanup.schedule: "@yearly"
      ofelia.job-exec.cleanup.command: "cleanup.sh"

      # Alert job (triggered on failure)
      ofelia.job-exec.alert.schedule: "@yearly"
      ofelia.job-exec.alert.command: "alert.sh"
```

### Important Notes

1. **Circular dependencies are detected** - Ofelia will reject configurations with circular dependency chains
2. **Dependencies must exist** - Referenced jobs must be defined in the configuration
3. **All job types supported** - Dependencies work across all job types (exec, run, local, service, compose)
4. **Multiple dependencies** - Use multiple `depends-on` lines in INI format to specify multiple dependencies

## Security Considerations

### Restricting Host Jobs

```ini
[global]
# Prevent LocalJobs from Docker labels
allow-host-jobs-from-labels = false
```

### JWT Configuration

```ini
[global]
# JWT for API authentication
jwt-secret = ${JWT_SECRET}  # From environment
jwt-expiry-hours = 24
jwt-refresh-enabled = true
jwt-refresh-hours = 168  # 1 week
```

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