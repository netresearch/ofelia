# Ofelia Integration Patterns

Real-world integration scenarios, best practices, and configuration patterns for common use cases.

## Table of Contents
- [Database Management](#database-management)
- [Log Management](#log-management)
- [Backup & Recovery](#backup--recovery)
- [Health Monitoring](#health-monitoring)
- [Data Synchronization](#data-synchronization)
- [Cleanup & Maintenance](#cleanup--maintenance)
- [CI/CD Integration](#cicd-integration)
- [Multi-Environment Patterns](#multi-environment-patterns)
- [Performance Optimization](#performance-optimization)
- [Security Patterns](#security-patterns)

---

## Database Management

### Pattern: Automated Database Backups

**Scenario**: Daily PostgreSQL backups with retention management and Slack notifications.

```ini
[global]
slack-webhook = https://hooks.slack.com/services/YOUR/WEBHOOK/URL
slack-only-on-error = false

[job-exec "postgres-backup"]
schedule = 0 2 * * *  # 2 AM daily
container = postgres
command = pg_dumpall -U postgres | gzip > /backups/backup_$(date +\%Y\%m\%d_\%H\%M\%S).sql.gz
user = postgres
no-overlap = true
history-limit = 30

[job-exec "backup-cleanup"]
schedule = 0 3 * * *  # 3 AM daily
container = postgres
command = find /backups -name "*.sql.gz" -mtime +30 -delete
```

**Docker Compose Integration**:
```yaml
version: "3.8"
services:
  postgres:
    image: postgres:15
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./backups:/backups
    labels:
      ofelia.enabled: "true"
      ofelia.job-exec.db-backup.schedule: "0 2 * * *"
      ofelia.job-exec.db-backup.command: "pg_dumpall -U postgres | gzip > /backups/backup_$(date +%Y%m%d).sql.gz"
      ofelia.job-exec.db-backup.user: "postgres"
  
  ofelia:
    image: ghcr.io/netresearch/ofelia:latest
    depends_on:
      - postgres
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    labels:
      ofelia.slack-webhook: "https://hooks.slack.com/services/YOUR/WEBHOOK"
```

### Pattern: Database Maintenance

**Scenario**: Weekly vacuum and analyze for optimal performance.

```ini
[job-exec "postgres-vacuum"]
schedule = 0 1 * * 0  # Sunday 1 AM
container = postgres
command = vacuumdb --all --analyze --verbose
user = postgres
max-runtime = 2h
no-overlap = true

[job-exec "postgres-reindex"]
schedule = 0 1 1 * *  # First of month, 1 AM
container = postgres
command = reindexdb --all --verbose
user = postgres
max-runtime = 4h
no-overlap = true
```

**Best Practices**:
- Set `no-overlap = true` for long-running maintenance
- Use `max-runtime` to prevent runaway jobs
- Schedule during low-traffic periods
- Monitor execution time trends via metrics

---

## Log Management

### Pattern: Log Rotation and Archiving

**Scenario**: Hourly log compression and daily archiving to S3.

```ini
[job-local "compress-logs"]
schedule = 0 * * * *  # Hourly
command = find /var/log/myapp -name "*.log" -mmin +60 -exec gzip {} \;
dir = /var/log
no-overlap = true

[job-run "archive-to-s3"]
schedule = 0 4 * * *  # Daily at 4 AM
image = amazon/aws-cli:latest
command = aws s3 sync /logs s3://mybucket/logs/$(date +\%Y-\%m-\%d)/
volume = /var/log/myapp:/logs:ro
environment = AWS_ACCESS_KEY_ID=${AWS_KEY}
environment = AWS_SECRET_ACCESS_KEY=${AWS_SECRET}
delete = true
```

### Pattern: Log Analysis and Alerting

**Scenario**: Detect error patterns and send alerts.

```ini
[job-exec "analyze-errors"]
schedule = */15 * * * *  # Every 15 minutes
container = app
command = sh -c 'error_count=$(grep -c "ERROR" /var/log/app.log | tail -100); if [ $error_count -gt 10 ]; then echo "High error rate: $error_count errors"; exit 1; fi'
```

**With Email Notifications**:
```ini
[global]
smtp-host = smtp.gmail.com
smtp-port = 587
smtp-user = alerts@example.com
smtp-password = ${SMTP_PASSWORD}
email-to = team@example.com
email-from = ofelia@example.com
mail-only-on-error = true

[job-exec "error-monitor"]
schedule = */5 * * * *
container = app
command = /scripts/check-error-rate.sh
```

---

## Backup & Recovery

### Pattern: Multi-Tier Backup Strategy

**Scenario**: Incremental backups hourly, full backups daily, with off-site replication.

```ini
[job-run "incremental-backup"]
schedule = 0 * * * *  # Hourly
image = restic/restic:latest
command = backup --files-from /backup-list.txt
volume = /data:/data:ro
volume = /backup-cache:/root/.cache/restic
environment = RESTIC_REPOSITORY=/backups/incremental
environment = RESTIC_PASSWORD=${BACKUP_PASSWORD}
no-overlap = true
max-runtime = 30m

[job-run "full-backup"]
schedule = 0 3 * * *  # Daily at 3 AM
image = restic/restic:latest
command = backup --files-from /backup-list.txt --force
volume = /data:/data:ro
volume = /backup-cache:/root/.cache/restic
environment = RESTIC_REPOSITORY=/backups/full
environment = RESTIC_PASSWORD=${BACKUP_PASSWORD}
no-overlap = true
max-runtime = 2h

[job-run "backup-verification"]
schedule = 0 5 * * *  # Daily at 5 AM
image = restic/restic:latest
command = check --read-data-subset=10%
environment = RESTIC_REPOSITORY=/backups/full
environment = RESTIC_PASSWORD=${BACKUP_PASSWORD}
max-runtime = 1h

[job-run "offsite-sync"]
schedule = 0 6 * * *  # Daily at 6 AM
image = rclone/rclone:latest
command = sync /backups remote:backups --progress
volume = /backups:/backups:ro
volume = /root/.config/rclone:/config/rclone
max-runtime = 3h
```

---

## Health Monitoring

### Pattern: Service Health Checks

**Scenario**: Regular health checks with automatic restarts and notifications.

```ini
[job-exec "app-health-check"]
schedule = */2 * * * *  # Every 2 minutes
container = web-app
command = curl -f http://localhost:8080/health || exit 1

[job-exec "database-health"]
schedule = */5 * * * *  # Every 5 minutes
container = postgres
command = pg_isready -U postgres
user = postgres

[job-run "external-endpoint-check"]
schedule = */10 * * * *  # Every 10 minutes
image = curlimages/curl:latest
command = curl -f --max-time 10 https://api.example.com/health
```

**With Automatic Recovery**:
```yaml
services:
  app:
    image: myapp:latest
    labels:
      ofelia.enabled: "true"
      ofelia.job-exec.health.schedule: "*/2 * * * *"
      ofelia.job-exec.health.command: "/health-check.sh"
    healthcheck:
      test: ["CMD", "/health-check.sh"]
      interval: 30s
      timeout: 10s
      retries: 3
    restart: unless-stopped
```

### Pattern: Resource Monitoring

**Scenario**: Track disk usage, memory, and send alerts when thresholds exceeded.

```ini
[job-local "disk-monitor"]
schedule = */30 * * * *
command = df -h | awk '$5+0 > 80 {print "WARNING: "$0; exit 1}'

[job-exec "memory-monitor"]
schedule = */15 * * * *
container = app
command = sh -c 'mem=$(free | awk "NR==2{printf \"%.0f\", $3*100/$2}"); if [ $mem -gt 90 ]; then echo "High memory: ${mem}%"; exit 1; fi'
```

---

## Data Synchronization

### Pattern: Database Replication Monitoring

**Scenario**: Monitor PostgreSQL replication lag and alert on issues.

```ini
[job-exec "check-replication-lag"]
schedule = */5 * * * *
container = postgres-primary
command = psql -U postgres -c "SELECT CASE WHEN pg_last_wal_receive_lsn() = pg_last_wal_replay_lsn() THEN 0 ELSE EXTRACT(EPOCH FROM now() - pg_last_xact_replay_timestamp()) END AS lag;" | awk 'NR==3 {if ($1 > 60) exit 1}'
user = postgres
```

### Pattern: File Synchronization

**Scenario**: Sync files between containers or to external storage.

```ini
[job-run "sync-uploads"]
schedule = */10 * * * *
image = alpine:latest
command = rsync -av --delete /source/ /destination/
volume = /app/uploads:/source:ro
volume = /shared/uploads:/destination
no-overlap = true

[job-compose "sync-to-cdn"]
schedule = */5 * * * *
file = docker-compose.yml
service = cdn-sync
command = /scripts/sync-to-cdn.sh
exec = false
```

---

## Cleanup & Maintenance

### Pattern: Container and Image Cleanup

**Scenario**: Remove old Docker images and volumes to reclaim space.

```ini
[job-local "docker-cleanup"]
schedule = 0 3 * * 0  # Sunday 3 AM
command = docker system prune -af --volumes --filter "until=168h"
environment = DOCKER_HOST=unix:///var/run/docker.sock

[job-local "image-cleanup"]
schedule = 0 4 * * 0  # Sunday 4 AM
command = docker images --filter "dangling=true" -q | xargs -r docker rmi
```

### Pattern: Temporary File Cleanup

**Scenario**: Clean temporary files and caches regularly.

```ini
[job-exec "temp-cleanup"]
schedule = 0 2 * * *
container = app
command = find /tmp -type f -mtime +7 -delete

[job-exec "cache-cleanup"]
schedule = 0 3 * * *
container = app
command = find /var/cache/app -type f -mtime +1 -delete

[job-local "system-cache"]
schedule = 0 1 * * 0  # Sunday 1 AM
command = find /var/tmp -type f -mtime +30 -delete
dir = /
```

---

## CI/CD Integration

### Pattern: Automated Deployments

**Scenario**: Pull latest images and redeploy on schedule.

```ini
[job-local "deploy-latest"]
schedule = 0 4 * * *  # Daily at 4 AM
command = docker-compose pull && docker-compose up -d --remove-orphans
dir = /opt/myapp
environment = COMPOSE_PROJECT_NAME=myapp

[job-local "health-check-after-deploy"]
schedule = 15 4 * * *  # 15 minutes after deploy
command = curl -f http://localhost:8080/health --retry 5 --retry-delay 10
```

### Pattern: Scheduled E2E Tests

**Scenario**: Run end-to-end tests against staging environment.

```ini
[job-run "e2e-tests"]
schedule = 0 */6 * * *  # Every 6 hours
image = my-test-runner:latest
command = npm test
environment = TEST_ENV=staging
environment = API_URL=https://staging.api.example.com
delete = true
max-runtime = 30m
```

---

## Multi-Environment Patterns

### Pattern: Environment-Specific Jobs

**Scenario**: Different schedules for dev, staging, production.

```ini
# Production
[job-exec "prod-backup"]
schedule = 0 2 * * *
container = prod-db
command = pg_dumpall -U postgres > /backups/prod_$(date +\%Y\%m\%d).sql

# Staging
[job-exec "staging-backup"]
schedule = 0 4 * * *
container = staging-db
command = pg_dumpall -U postgres > /backups/staging_$(date +\%Y\%m\%d).sql

# Development
[job-exec "dev-backup"]
schedule = 0 6 * * 0  # Weekly
container = dev-db
command = pg_dumpall -U postgres > /backups/dev_$(date +\%Y\%m\%d).sql
```

**Using Environment Variables**:
```yaml
services:
  ofelia:
    image: ghcr.io/netresearch/ofelia:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      - OFELIA_CONFIG=/etc/ofelia/${ENVIRONMENT}.ini
    labels:
      ofelia.job-exec.backup.schedule: "${BACKUP_SCHEDULE:-0 2 * * *}"
      ofelia.job-exec.backup.container: "${DB_CONTAINER:-postgres}"
```

---

## Performance Optimization

### Pattern: Rate-Limited Processing

**Scenario**: Process queue with rate limiting to avoid overwhelming downstream services.

```ini
[job-exec "process-queue"]
schedule = * * * * *  # Every minute
container = worker
command = /scripts/process-batch.sh --limit 100
no-overlap = true
max-runtime = 55s

[job-exec "large-batch-processing"]
schedule = 0 2 * * *  # Daily at 2 AM
container = worker
command = /scripts/process-all.sh --batch-size 1000
no-overlap = true
max-runtime = 4h
```

### Pattern: Concurrent Job Execution

**Scenario**: Multiple independent jobs running in parallel.

```ini
# These jobs run concurrently
[job-run "process-region-us"]
schedule = 0 3 * * *
image = data-processor:latest
command = process --region us
environment = REGION=us

[job-run "process-region-eu"]
schedule = 0 3 * * *
image = data-processor:latest
command = process --region eu
environment = REGION=eu

[job-run "process-region-asia"]
schedule = 0 3 * * *
image = data-processor:latest
command = process --region asia
environment = REGION=asia
```

---

## Security Patterns

### Pattern: Secret Management

**Scenario**: Use environment variables and Docker secrets for sensitive data.

```yaml
services:
  ofelia:
    image: ghcr.io/netresearch/ofelia:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      - DB_PASSWORD_FILE=/run/secrets/db_password
      - API_KEY_FILE=/run/secrets/api_key
    secrets:
      - db_password
      - api_key
    labels:
      ofelia.job-exec.backup.schedule: "0 2 * * *"
      ofelia.job-exec.backup.command: "pg_dump -U postgres $(cat $DB_PASSWORD_FILE)"

secrets:
  db_password:
    external: true
  api_key:
    external: true
```

### Pattern: Audit Logging

**Scenario**: Log all job executions with detailed audit trail.

```ini
[global]
save-folder = /var/log/ofelia
save-only-on-error = false  # Log all executions

[job-exec "sensitive-operation"]
schedule = 0 3 * * *
container = app
command = /scripts/admin-task.sh
user = admin
```

**With Slack Audit Trail**:
```ini
[global]
slack-webhook = https://hooks.slack.com/services/YOUR/AUDIT/WEBHOOK
slack-only-on-error = false  # Notify all executions

[job-exec "user-data-export"]
schedule = 0 4 * * 0  # Weekly
container = app
command = /scripts/export-user-data.sh --admin-initiated
user = admin
```

### Pattern: Network Isolation

**Scenario**: Run jobs in isolated network for security.

```ini
[job-run "untrusted-processing"]
schedule = */30 * * * *
image = processor:latest
command = /scripts/process.sh
network = isolated_network
delete = true
```

---

## Troubleshooting Common Patterns

### Pattern: Debug Failed Jobs

**Enable verbose logging**:
```ini
[global]
log-level = DEBUG

[job-exec "failing-job"]
schedule = @every 5m
container = app
command = /scripts/debug.sh
```

**Capture full output**:
```ini
[global]
save-folder = /var/log/ofelia-debug
save-only-on-error = false

[job-exec "debug-job"]
schedule = @every 10m
container = app
command = sh -c 'set -x; /scripts/task.sh 2>&1'
```

### Pattern: Job Stuck Prevention

**Use timeouts and no-overlap**:
```ini
[job-exec "long-running"]
schedule = 0 * * * *
container = worker
command = /scripts/process.sh
no-overlap = true  # Prevent concurrent execution
max-runtime = 50m  # Kill after 50 minutes
```

### Pattern: Gradual Rollout

**Test new jobs with conservative scheduling**:
```ini
# Phase 1: Test hourly
[job-exec "new-feature-test"]
schedule = 0 * * * *
container = app
command = /scripts/new-feature.sh --dry-run

# Phase 2: Production every 6 hours
# [job-exec "new-feature-prod"]
# schedule = 0 */6 * * *
# container = app
# command = /scripts/new-feature.sh

# Phase 3: Production every hour (uncomment when stable)
```

---

## Best Practices Summary

### Scheduling
✅ Use `no-overlap = true` for long-running or resource-intensive jobs
✅ Set appropriate `max-runtime` to prevent runaway jobs
✅ Schedule maintenance during low-traffic periods
✅ Use cron expressions that distribute load (avoid XX:00 pileups)

### Resource Management
✅ Use `delete = true` for RunJobs to avoid container accumulation
✅ Set appropriate `history-limit` to manage memory
✅ Monitor job execution times via Prometheus metrics
✅ Use `volume` mounts for persistent data

### Error Handling
✅ Enable error notifications (email, Slack) for critical jobs
✅ Use `save-folder` to persist execution logs
✅ Implement health checks and monitoring
✅ Set up alert thresholds appropriate to job criticality

### Security
✅ Use environment variables for secrets (never hardcode)
✅ Apply principle of least privilege (`user` parameter)
✅ Use read-only volume mounts where possible
✅ Implement audit logging for sensitive operations
✅ Isolate untrusted workloads with separate networks

### Testing
✅ Test jobs with `@every 1m` during development
✅ Use `--validate` command to check configuration
✅ Monitor first few executions closely
✅ Implement gradual rollout for new jobs

---

## Cross-References

- [Configuration Guide](./CONFIGURATION.md)
- [Jobs Reference](./jobs.md)
- [Architecture Diagrams](./ARCHITECTURE_DIAGRAMS.md)
- [Troubleshooting Guide](./TROUBLESHOOTING.md)
- [Security Documentation](./SECURITY.md)
- [API Documentation](./API.md)

---

*Generated: 2025-11-21 | Real-world integration patterns and best practices for Ofelia*
