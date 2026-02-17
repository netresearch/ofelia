# Ofelia Quick Reference

Fast lookup guide for common commands, configurations, and troubleshooting.

## CLI Commands

```bash
# Daemon
ofelia daemon                              # Start with default config
ofelia daemon --config=/path/to/config.ini # Custom config
ofelia daemon --log-level=DEBUG            # Enable debug logging
ofelia daemon --enable-web --web-address=:8081  # Enable web UI
ofelia daemon --docker-events              # Use Docker events instead of polling

# Validation
ofelia validate                            # Validate default config
ofelia validate --config=/path/to/config.ini  # Validate specific config

# Help
ofelia --help                              # Show help
ofelia daemon --help                       # Daemon-specific help
```

## Configuration Locations

```bash
# Default INI location
/etc/ofelia/config.ini

# Glob patterns supported
/etc/ofelia/conf.d/*.ini

# Docker socket
/var/run/docker.sock  # Mount as ro
```

## Environment Variables

```bash
# Configuration
export OFELIA_CONFIG=/path/to/config.ini
export OFELIA_LOG_LEVEL=DEBUG              # DEBUG, INFO, WARNING, ERROR

# Docker
export OFELIA_DOCKER_FILTER="label=env=prod"  # Filter containers
export OFELIA_POLL_INTERVAL=30s            # Deprecated: legacy poll-interval (no default)
export OFELIA_DOCKER_EVENTS=true           # Use events
export OFELIA_DOCKER_NO_POLL=true          # Disable polling

# Web UI
export OFELIA_ENABLE_WEB=true
export OFELIA_WEB_ADDRESS=:8081

# pprof
export OFELIA_ENABLE_PPROF=true
export OFELIA_PPROF_ADDRESS=127.0.0.1:8080
```

## Cron Schedule Formats

```bash
# Standard cron (5 fields)
0 2 * * *                  # Every day at 2:00 AM
0 */6 * * *                # Every 6 hours
*/15 * * * *               # Every 15 minutes
0 0 1 * *                  # First day of every month

# Quartz format (6 fields with seconds)
0 30 2 * * *               # Every day at 2:30:00 AM
0 0 */4 * * *              # Every 4 hours
0 */30 * * * *             # Every 30 minutes

# With year field (one-time scheduling)
30 14 25 12 * 2025         # Dec 25, 2025 at 14:30 (one-time job)
0 30 14 25 12 * 2025       # Same with seconds field
0 0 1 1 * 2025-2027        # Jan 1 at midnight, 2025-2027

# Extended syntax (L, W, #)
0 12 L * *                 # Last day of every month at noon
0 12 15W * *               # Nearest weekday to 15th at noon
0 12 * * FRI#3             # 3rd Friday of every month at noon
0 12 * * FRI#L             # Last Friday of every month at noon

# Descriptors
@yearly                    # 0 0 1 1 *
@annually                  # 0 0 1 1 *
@monthly                   # 0 0 1 * *
@weekly                    # 0 0 * * 0
@daily                     # 0 0 * * *
@midnight                  # 0 0 * * *
@hourly                    # 0 * * * *
@every 5s                  # Every 5 seconds
@every 1h30m               # Every 1.5 hours
```

## Job Types Cheat Sheet

### ExecJob (Existing Container)
```ini
[job-exec "name"]
schedule = @daily
container = container-name
command = /path/to/script.sh
user = nobody                   # Optional (default: nobody)
tty = false                     # Optional
working-dir = /app              # Optional (Docker 17.09+)
environment = FOO=bar           # Optional (Docker API 1.30+)
no-overlap = true               # Optional
history-limit = 10              # Optional
```

### RunJob (New Container)
```ini
[job-run "name"]
schedule = @hourly
image = alpine:latest
command = echo "hello"
user = nobody                   # Optional (default: nobody)
network = bridge                # Optional
hostname = job-container        # Optional
volume = /host:/container:ro    # Optional
environment = FOO=bar           # Optional
delete = true                   # Optional (default: true)
no-overlap = true               # Optional
max-runtime = 24h               # Optional
```

### LocalJob (Host Execution)
```ini
[job-local "name"]
schedule = @daily
command = /usr/local/bin/backup.sh
dir = /opt/app                  # Optional
environment = FOO=bar           # Optional
no-overlap = true               # Optional
```

### ServiceJob (Docker Swarm)
```ini
[job-service-run "name"]
schedule = @daily
image = alpine:latest
command = echo "hello"
network = swarm_network         # Required for swarm
user = nobody                   # Optional (default: nobody)
no-overlap = true               # Optional
max-runtime = 24h               # Optional
```

### ComposeJob
```ini
[job-compose "name"]
schedule = @daily
file = docker-compose.yml       # Optional (default: compose.yml)
service = web                   # Required
command = /scripts/task.sh      # Optional
exec = false                    # Optional (false=run, true=exec)
```

## Docker Labels Format

```bash
# Enable Ofelia on container
ofelia.enabled=true

# Basic job
ofelia.job-exec.<job-name>.schedule="@daily"
ofelia.job-exec.<job-name>.command="/script.sh"

# Full example
docker run -d \
  --label ofelia.enabled=true \
  --label ofelia.job-exec.backup.schedule="@daily" \
  --label ofelia.job-exec.backup.command="pg_dump mydb" \
  --label ofelia.job-exec.backup.user="postgres" \
  postgres:15

# Multiple environment variables (JSON array)
ofelia.job-run.task.environment='["FOO=bar", "BAZ=qux"]'

# Multiple volumes (JSON array)
ofelia.job-run.task.volume='["/host1:/container1:ro", "/host2:/container2:rw"]'

# Global settings on Ofelia container
ofelia.log-level=DEBUG
ofelia.slack-webhook=https://hooks.slack.com/...
ofelia.enable-web=true
ofelia.web-address=:8081
```

## Middleware Configuration

### Webhooks (Recommended)
```ini
# Define named webhooks
[webhook "slack-alerts"]
preset = slack
id = T00000000/B00000000000
secret = XXXXXXXXXXXXXXXXXXXXXXXX
trigger = error

[webhook "discord"]
preset = discord
id = 1234567890123456789
secret = abcdefghijklmnopqrstuvwxyz
trigger = always

# Assign to jobs
[job-exec "backup"]
schedule = @daily
container = postgres
command = pg_dump mydb
webhooks = slack-alerts, discord
```

Docker labels:
```bash
# Webhook labels (on service container with ofelia.service=true)
ofelia.webhook.slack.preset=slack
ofelia.webhook.slack.id=T00000000/B00000000000
ofelia.webhook.slack.secret=XXXXXXXXXXXXXXXXXXXXXXXX
ofelia.webhook.slack.trigger=error
ofelia.webhook.slack.timeout=30s
ofelia.webhook.slack.retry-count=3
ofelia.webhook.slack.link=https://logs.example.com
ofelia.webhook.slack.link-text=View Logs
# Global webhook settings (on service container)
ofelia.webhooks=slack
ofelia.webhook-allowed-hosts=hooks.slack.com
# Assign webhook to job (on any container)
ofelia.job-exec.backup.webhooks=slack
```

**Bundled Presets**: `slack`, `discord`, `teams`, `matrix`, `ntfy`, `pushover`, `pagerduty`, `gotify`

**Triggers**: `always` (default), `error`, `success`

> See [Webhook Documentation](./webhooks.md) for full reference.

### Email
```ini
[global]
smtp-host = smtp.gmail.com
smtp-port = 587
smtp-user = alerts@example.com
smtp-password = secret
email-to = team@example.com
email-from = ofelia@example.com
mail-only-on-error = true      # Only send on failure
```

### Slack (Deprecated)
```ini
# DEPRECATED - Use [webhook "name"] sections instead
[global]
slack-webhook = https://hooks.slack.com/services/...
slack-only-on-error = false     # Send all notifications
```

### Save Reports
```ini
[global]
save-folder = /var/log/ofelia
save-only-on-error = false      # Save all executions
```

## Docker Compose Example

```yaml
version: "3.8"
services:
  ofelia:
    image: ghcr.io/netresearch/ofelia:latest
    depends_on:
      - app
    command: daemon --docker-events
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./ofelia.ini:/etc/ofelia/config.ini:ro
    labels:
      ofelia.log-level: "INFO"
      ofelia.enable-web: "true"
      ofelia.web-address: ":8081"
    ports:
      - "8081:8081"

  app:
    image: myapp:latest
    labels:
      ofelia.enabled: "true"
      ofelia.job-exec.health.schedule: "*/5 * * * *"
      ofelia.job-exec.health.command: "curl -f localhost:8080/health"
```

## API Endpoints

```bash
# Authentication
POST   /api/login              # Login (get JWT)
POST   /api/refresh            # Refresh token
POST   /api/logout             # Logout

# Jobs
GET    /api/jobs               # List all jobs
GET    /api/jobs/{name}        # Get job details
POST   /api/jobs/{name}/run    # Trigger job manually
PUT    /api/jobs/{name}        # Update job
DELETE /api/jobs/{name}        # Remove job
GET    /api/jobs/{name}/history  # Execution history
GET    /api/jobs/removed       # Removed jobs

# Config & Health
GET    /api/config             # Current configuration
GET    /health                 # Health check
GET    /metrics                # Prometheus metrics
```

### API Usage Examples

```bash
# Login
curl -X POST http://localhost:8081/api/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "secret"}'

# List jobs (with JWT)
curl http://localhost:8081/api/jobs \
  -H "Authorization: Bearer <token>"

# Trigger job
curl -X POST http://localhost:8081/api/jobs/backup/run \
  -H "Authorization: Bearer <token>"

# Get job history
curl http://localhost:8081/api/jobs/backup/history \
  -H "Authorization: Bearer <token>"
```

## Troubleshooting Quick Fixes

### Job Not Running
```bash
# 1. Check Ofelia logs
docker logs ofelia

# 2. Validate configuration
ofelia validate --config=/etc/ofelia/config.ini

# 3. Check job schedule
# Use https://crontab.guru/ to verify cron expression

# 4. Check container labels
docker inspect <container> | grep ofelia
```

### Docker Socket Issues
```bash
# Check socket permissions
ls -la /var/run/docker.sock

# Test Docker access from Ofelia container
docker exec ofelia docker ps

# Ensure socket is mounted
docker inspect ofelia | grep -A5 Mounts
```

### Configuration Not Reloading
```bash
# Check if polling is enabled
# Look for "docker-no-poll" or "poll-interval" settings

# Force reload by restarting
docker restart ofelia

# Enable events instead of polling
docker run -d --name ofelia \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/netresearch/ofelia:latest daemon --docker-events
```

### High CPU Usage
```bash
# Reduce polling frequency
export OFELIA_POLL_INTERVAL=60s

# Or switch to event-based monitoring
export OFELIA_DOCKER_EVENTS=true

# Enable pprof for profiling
export OFELIA_ENABLE_PPROF=true
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof
```

### Jobs Overlapping
```bash
# Add no-overlap to job config
[job-exec "long-task"]
schedule = @hourly
container = worker
command = /scripts/task.sh
no-overlap = true               # ← Add this
max-runtime = 50m               # ← And timeout
```

## Performance Tips

```bash
# 1. Use event-based monitoring instead of polling
OFELIA_DOCKER_EVENTS=true

# 2. Increase polling interval if needed
OFELIA_POLL_INTERVAL=30s

# 3. Use no-overlap for long-running jobs
no-overlap = true

# 4. Set appropriate max-runtime
max-runtime = 1h

# 5. Limit history to reduce memory
history-limit = 10

# 6. Use delete=true for RunJobs
delete = true
```

## Common Patterns

### Database Backup
```ini
[job-exec "db-backup"]
schedule = 0 2 * * *            # 2 AM daily
container = postgres
command = pg_dumpall -U postgres | gzip > /backups/$(date +\%Y\%m\%d).sql.gz
user = postgres
no-overlap = true
max-runtime = 2h
```

### Log Rotation
```ini
[job-local "rotate-logs"]
schedule = 0 0 * * *            # Midnight
command = find /var/log/app -name "*.log" -mtime +7 -delete
dir = /var/log
```

### Health Check
```ini
[job-exec "health"]
schedule = */5 * * * *          # Every 5 minutes
container = app
command = curl -f http://localhost:8080/health || exit 1
```

### Cleanup
```ini
[job-local "docker-cleanup"]
schedule = 0 3 * * 0            # Sunday 3 AM
command = docker system prune -af --volumes
```

### Redis Cache Flush
```ini
[job-exec "redis-flush"]
schedule = 0 4 * * *            # 4 AM daily
container = redis
command = redis-cli FLUSHDB
```

### SSL Certificate Renewal
```ini
[job-run "certbot-renew"]
schedule = 0 0 1 * *            # 1st of each month
image = certbot/certbot:latest
command = renew --webroot -w /var/www/html
volume = /etc/letsencrypt:/etc/letsencrypt:rw
volume = /var/www/html:/var/www/html:ro
delete = true
```

## Docker Compose Examples

### Minimal Setup (Labels Only)
```yaml
version: '3.8'
services:
  ofelia:
    image: netresearch/ofelia:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    labels:
      ofelia.enabled: "true"
      ofelia.job-run.cleanup.schedule: "@daily"
      ofelia.job-run.cleanup.image: "alpine:latest"
      ofelia.job-run.cleanup.command: "echo 'Hello from Ofelia'"
      ofelia.job-run.cleanup.delete: "true"
```

### With Web UI and Notifications
```yaml
version: '3.8'
services:
  ofelia:
    image: netresearch/ofelia:latest
    command: daemon --config=/etc/ofelia/config.ini
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./ofelia.ini:/etc/ofelia/config.ini:ro
    environment:
      - OFELIA_ENABLE_WEB=true
      - OFELIA_WEB_ADDRESS=:8080
      - OFELIA_LOG_LEVEL=INFO
    ports:
      - "8080:8080"

  app:
    image: myapp:latest
    labels:
      ofelia.enabled: "true"
      ofelia.job-exec.task.schedule: "@every 1h"
      ofelia.job-exec.task.command: "php artisan schedule:run"
```

### Multi-Container Stack
```yaml
version: '3.8'
services:
  ofelia:
    image: netresearch/ofelia:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro

  postgres:
    image: postgres:15
    labels:
      ofelia.enabled: "true"
      ofelia.job-exec.backup.schedule: "0 2 * * *"
      ofelia.job-exec.backup.command: "pg_dump -U postgres mydb > /backup/db.sql"
      ofelia.job-exec.backup.user: "postgres"
      ofelia.job-exec.vacuum.schedule: "@weekly"
      ofelia.job-exec.vacuum.command: "vacuumdb --all --analyze"

  redis:
    image: redis:7
    labels:
      ofelia.enabled: "true"
      ofelia.job-exec.flush.schedule: "@daily"
      ofelia.job-exec.flush.command: "redis-cli FLUSHDB"
```

## Notification Examples

### Webhook Notifications (Recommended)
```ini
# Multi-service webhook notifications
[webhook "slack-alerts"]
preset = slack
id = T00000000/B00000000000
secret = XXXXXXXXXXXXXXXXXXXXXXXX
trigger = error

[webhook "discord-all"]
preset = discord
id = 1234567890123456789
secret = abcdefghijklmnopqrstuvwxyz
trigger = always

[webhook "pagerduty-critical"]
preset = pagerduty
secret = your-routing-key
trigger = error

[job-exec "backup"]
schedule = @daily
container = postgres
command = pg_dump mydb > /backup/db.sql
webhooks = slack-alerts, discord-all
```

### Slack Notification (Deprecated)
```ini
# DEPRECATED - Use [webhook "name"] sections instead
[global]
slack-webhook = https://hooks.slack.com/services/XXX/YYY/ZZZ
slack-only-on-error = true

[job-exec "backup"]
schedule = @daily
container = postgres
command = pg_dump mydb > /backup/db.sql
```

### Email Notification with TLS
```ini
[global]
smtp-host = smtp.gmail.com
smtp-port = 587
smtp-user = alerts@example.com
smtp-password = ${SMTP_PASSWORD}
email-from = alerts@example.com
email-to = admin@example.com

[job-exec "critical-task"]
schedule = @hourly
container = app
command = /critical-job.sh
mail-only-on-error = false          # Get success notifications too
```

### Both Slack and Email
```ini
[job-exec "important"]
schedule = @daily
container = app
command = /important-task.sh

# Slack for quick alerts
slack-webhook = https://hooks.slack.com/...
slack-channel = #ops
slack-only-on-error = true

# Email for detailed logs
email-to = team@example.com
mail-only-on-error = false
```

## Security Checklist

- [ ] Mount Docker socket as read-only (`:ro`)
- [ ] Use environment variables for secrets (never hardcode)
- [ ] Enable JWT authentication for web UI
- [ ] Set appropriate user for exec/run jobs
- [ ] Use read-only volume mounts where possible
- [ ] Enable audit logging (`save-only-on-error = false`)
- [ ] Set `max-runtime` to prevent runaway jobs
- [ ] Use `no-overlap` for resource-intensive tasks
- [ ] Validate all input in custom scripts
- [ ] Keep Ofelia image updated

## Useful Links

- [Full Documentation](../README.md)
- [Configuration Guide](./CONFIGURATION.md)
- [Webhook Notifications](./webhooks.md)
- [Jobs Reference](./jobs.md)
- [API Documentation](./API.md)
- [Integration Patterns](./INTEGRATION_PATTERNS.md)
- [Architecture Diagrams](./ARCHITECTURE_DIAGRAMS.md)
- [Troubleshooting](./TROUBLESHOOTING.md)
- [Cron Expression Tester](https://crontab.guru/)

---

*Generated: 2025-11-21 | Quick reference for common Ofelia operations*
