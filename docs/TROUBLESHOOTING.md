# Troubleshooting Guide

**Last Updated**: 2025-01-15

## Overview

This guide provides solutions to common issues encountered when deploying, configuring, and operating Ofelia. Issues are organized by category for quick reference.

## Quick Diagnostics

### Health Check
```bash
# Check overall health
curl http://localhost:8080/health

# Check readiness (503 if unhealthy)
curl http://localhost:8080/ready

# Check liveness (always 200 if running)
curl http://localhost:8080/live
```

### Logs Analysis
```bash
# Docker logs
docker logs ofelia

# Follow logs
docker logs -f ofelia

# Last 100 lines
docker logs --tail 100 ofelia

# With timestamps
docker logs -t ofelia
```

### Configuration Validation
```bash
# Validate config file
ofelia validate --config=/etc/ofelia/config.ini

# Test specific job
ofelia test --config=/etc/ofelia/config.ini --job=backup

# Dry run
ofelia daemon --config=/etc/ofelia/config.ini --dry-run
```

## Docker Issues

### Docker Daemon Not Accessible

**Symptoms**:
```
Error: Cannot connect to Docker daemon
Error: dial unix /var/run/docker.sock: connect: permission denied
```

**Diagnosis**:
```bash
# Check Docker daemon status
systemctl status docker

# Test Docker connection
docker ps

# Check socket permissions
ls -la /var/run/docker.sock
```

**Solutions**:

1. **Docker daemon not running**:
   ```bash
   sudo systemctl start docker
   sudo systemctl enable docker
   ```

2. **Permission denied**:
   ```bash
   # Add user to docker group
   sudo usermod -aG docker $USER
   newgrp docker

   # Or run with proper permissions
   sudo chown root:docker /var/run/docker.sock
   sudo chmod 660 /var/run/docker.sock
   ```

3. **Docker socket path incorrect**:
   ```ini
   [global]
   docker-host = unix:///var/run/docker.sock
   ```

4. **Docker Desktop not started** (macOS/Windows):
   - Start Docker Desktop application
   - Wait for "Docker Desktop is running" status

### Docker Socket with User Namespace Remapping

**Symptoms**:
```
Error: dial unix /var/run/docker.sock: connect: permission denied
```

This occurs when Docker is configured with user namespace remapping (`"userns-remap": "default"` in `/etc/docker/daemon.json`), which remaps container UIDs/GIDs for security isolation.

**Root Cause**:

User namespace remapping creates a mapping between container users and host users. The Ofelia container runs as a different effective UID on the host, which may not have permission to access the Docker socket.

**Diagnosis**:
```bash
# Check if userns-remap is enabled
docker info | grep "Docker Root Dir"
# If it shows /var/lib/docker/100000.100000, userns-remap is active

# Check the remapped user
grep dockremap /etc/subuid /etc/subgid

# Check socket ownership
ls -la /var/run/docker.sock

# Check Ofelia's effective UID in the container
docker exec ofelia id
```

**Solutions**:

1. **Match socket permissions to remapped user**:
   ```bash
   # Find the remapped UID (typically 100000 for dockremap)
   REMAP_UID=$(grep dockremap /etc/subuid | cut -d: -f2)

   # Grant access via ACL
   sudo setfacl -m u:$REMAP_UID:rwx /var/run/docker.sock
   ```

2. **Run Ofelia outside namespace remapping**:
   ```yaml
   # docker-compose.yml
   services:
     ofelia:
       image: ghcr.io/netresearch/ofelia:latest
       userns_mode: "host"  # Bypass namespace remapping
       volumes:
         - /var/run/docker.sock:/var/run/docker.sock:ro
   ```

3. **Use Docker TCP socket instead** (development only):

   > **WARNING**: This exposes the Docker daemon API without authentication.
   > Any local process can gain full Docker control, enabling privilege escalation
   > to root. Only use in isolated development environments, never in production.

   ```ini
   [global]
   docker-host = tcp://localhost:2375
   ```

   Enable TCP in Docker daemon:
   ```json
   {
     "hosts": ["unix:///var/run/docker.sock", "tcp://127.0.0.1:2375"]
   }
   ```

4. **Disable userns-remap for development**:
   ```json
   // /etc/docker/daemon.json
   {
     "userns-remap": ""
   }
   ```
   Then restart Docker: `sudo systemctl restart docker`

**Security Considerations**:
- `userns_mode: "host"` reduces container isolation
- TCP socket should only bind to localhost, not 0.0.0.0
- Consider the security implications before disabling userns-remap in production

### HTTP/2 Protocol Errors (v0.11.0 Only)

**Symptoms**:
```
Error: protocol error
Error: connection refused
Error: failed to connect to Docker daemon
```

**Affected Versions**: v0.11.0 only (fixed in v0.11.1+)

**Diagnosis**:
```bash
# Check Ofelia version
ofelia version

# Check Docker connection type
echo $DOCKER_HOST
# Common values:
# - unix:///var/run/docker.sock (default)
# - tcp://localhost:2375 (cleartext)
# - https://host:2376 (TLS)
```

**Root Cause**:
v0.11.0 introduced OptimizedDockerClient that incorrectly enabled HTTP/2 on all connections. Docker daemon only supports HTTP/2 over TLS (https://), not on:
- Unix domain sockets (unix://)
- TCP cleartext (tcp://)
- HTTP cleartext (http://)

**Solutions**:

1. **Upgrade to v0.11.1+** (Recommended):
   ```yaml
   # docker-compose.yml
   services:
     ofelia:
       image: mcuadros/ofelia:latest
   ```

2. **Downgrade to v0.10.2** (Temporary workaround):
   ```yaml
   services:
     ofelia:
       image: mcuadros/ofelia:v0.10.2
   ```

3. **Use HTTPS connection** (If possible):
   ```bash
   export DOCKER_HOST=https://docker-host:2376
   export DOCKER_CERT_PATH=/path/to/certs
   ```

**References**:
- Issue: #266
- Fix: #267
- Affected: Unix sockets, tcp://, http:// connections
- Working: https:// connections only

### Container Not Found

**Symptoms**:
```
Error: No such container: postgres
Error: Container postgres not found
```

**Diagnosis**:
```bash
# List all containers
docker ps -a

# Search for container
docker ps -a | grep postgres
```

**Solutions**:

1. **Container not running**:
   ```bash
   docker start postgres
   ```

2. **Wrong container name**:
   ```ini
   # Use exact container name or ID
   container = postgres_db_1
   # Or container ID
   container = abc123def456
   ```

3. **Container name changed**:
   ```bash
   # Check current name
   docker ps --format "{{.Names}}"

   # Update configuration
   [job-exec "backup"]
   container = new_postgres_name
   ```

### Image Pull Failures

**Symptoms**:
```
Error: Error response from daemon: pull access denied
Error: Error pulling image: manifest unknown
```

**Diagnosis**:
```bash
# Manual pull test
docker pull nginx:latest

# Check image name format
docker images | grep nginx
```

**Solutions**:

1. **Image not found**:
   ```ini
   # Verify image name
   image = nginx:1.21-alpine  # Correct
   # Not: image = nginx:invalid-tag
   ```

2. **Private registry authentication**:
   ```bash
   # Login to registry
   docker login registry.example.com

   # Or use credentials in config
   docker login -u username -p password registry.example.com
   ```

3. **Network connectivity**:
   ```bash
   # Test registry connectivity
   curl https://registry-1.docker.io/v2/

   # Check DNS resolution
   nslookup registry-1.docker.io
   ```

4. **Rate limiting (Docker Hub)**:
   ```
   Error: toomanyrequests: You have reached your pull rate limit

   Solution:
   - Login with Docker Hub account (increases limit)
   - Use alternative registry
   - Implement pull caching
   ```

## Configuration Issues

### Invalid Configuration Syntax

**Symptoms**:
```
Error: Config validation error: invalid syntax at line 15
Error: Unknown field 'schedual' in section [job-exec "backup"]
```

**Diagnosis**:
```bash
# Validate configuration
ofelia validate --config=/etc/ofelia/config.ini

# Check for typos
grep -i "schedual" /etc/ofelia/config.ini
```

**Solutions**:

1. **Typo in field name**:
   ```ini
   # Wrong
   schedual = @daily

   # Correct
   schedule = @daily
   ```

2. **Invalid INI syntax**:
   ```ini
   # Wrong - missing quotes
   [job-exec backup]

   # Correct
   [job-exec "backup"]
   ```

3. **Invalid value format**:
   ```ini
   # Wrong
   smtp-port = "587"  # Should be number, not string

   # Correct
   smtp-port = 587
   ```

### Invalid Cron Expression

**Symptoms**:
```
Error: Config validation error for field 'schedule': invalid cron expression
Error: Cron expression '0 */6 * *' is invalid: expected 5 or 6 fields
```

**Diagnosis**:
```bash
# Test cron expression
# Use online validator: crontab.guru
```

**Common Errors**:

| Invalid | Correct | Reason |
|---------|---------|--------|
| `0 */6 * *` | `0 */6 * * *` | Missing weekday field |
| `60 * * * *` | `0 * * * *` | Minutes: 0-59, not 60 |
| `* * 32 * *` | `* * 31 * *` | Day: 1-31, not 32 |
| `* * * 13 *` | `* * * 12 *` | Month: 1-12, not 13 |

**Solutions**:

1. **Standard cron (5 fields)**:
   ```ini
   schedule = 0 */6 * * *        # Every 6 hours
   schedule = 30 2 * * 0          # 2:30 AM every Sunday
   schedule = 0 0 1 * *           # Midnight on 1st of month
   ```

2. **Extended cron (6 fields with seconds)**:
   ```ini
   schedule = 0 0 */6 * * *       # Every 6 hours with seconds
   ```

3. **Special expressions**:
   ```ini
   schedule = @daily              # Once a day
   schedule = @hourly             # Once an hour
   schedule = @every 5m           # Every 5 minutes
   ```

### Environment Variable Not Resolved

**Symptoms**:
```
Error: JWT secret key must be at least 32 characters long
Warning: Using placeholder value "${JWT_SECRET}"
```

**Diagnosis**:
```bash
# Check environment variable
echo $JWT_SECRET

# Check in container
docker exec ofelia env | grep JWT_SECRET
```

**Solutions**:

1. **Environment variable not set**:
   ```bash
   # Set environment variable
   export JWT_SECRET="your-secret-key-here-min-32-chars"

   # Restart Ofelia
   docker restart ofelia
   ```

2. **Docker compose environment**:
   ```yaml
   services:
     ofelia:
       environment:
         - JWT_SECRET=${JWT_SECRET}
       # Or from .env file
       env_file:
         - .env
   ```

3. **Verify in container**:
   ```bash
   docker exec ofelia env | grep JWT_SECRET
   ```

### Job-Run Labels Not Discovered

**Symptoms**:
```
Error: unable to start a empty scheduler
```

Labels configured on application containers (nginx, postgres, etc.) with `job-run` jobs are not being detected by Ofelia.

**Root Cause**:

Different job types have different label placement requirements:

| Job Type | Label Placement | Reason |
|----------|----------------|--------|
| `job-exec` | Target container | Executes commands inside the labeled container |
| `job-run` | Ofelia container | Creates new containers, not tied to any specific existing container |
| `job-local` | Ofelia container | Runs on host, labels must be on Ofelia itself |
| `job-service` | Ofelia container | Creates Swarm services, not tied to existing containers |

**Incorrect Configuration** (Labels on nginx, not discovered):
```yaml
services:
  nginx:
    image: nginx:latest
    labels:
      ofelia.enabled: "true"
      # ❌ WRONG: job-run labels on nginx won't be discovered
      ofelia.job-run.backup.schedule: "@daily"
      ofelia.job-run.backup.image: "postgres:15"
      ofelia.job-run.backup.command: "pg_dump mydb"
```

**Correct Configuration** (Labels on Ofelia container):
```yaml
services:
  ofelia:
    image: netresearch/ofelia:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    labels:
      ofelia.enabled: "true"
      # ✅ CORRECT: job-run labels on Ofelia container
      ofelia.job-run.backup.schedule: "@daily"
      ofelia.job-run.backup.image: "postgres:15"
      ofelia.job-run.backup.command: "pg_dump mydb"

  nginx:
    image: nginx:latest
    labels:
      ofelia.enabled: "true"
      # ✅ CORRECT: job-exec labels on target container
      ofelia.job-exec.reload.schedule: "@hourly"
      ofelia.job-exec.reload.command: "nginx -s reload"
```

**Key Distinction**:
- `job-exec` requires a `container` parameter (implicit when using labels on target container)
- `job-run` requires an `image` parameter (creates new container, no existing container needed)

**Alternative**: Use INI configuration file instead of labels for `job-run` jobs, which avoids this confusion entirely.

## Authentication Issues

### JWT Secret Too Short

**Symptoms**:
```
Error: JWT secret key must be at least 32 characters long
Fatal: Cannot start server: invalid JWT configuration
```

**Solutions**:

1. **Generate proper secret**:
   ```bash
   # Generate 48-character base64 secret
   openssl rand -base64 48

   # Set in environment
   export OFELIA_JWT_SECRET="generated-secret-here"
   ```

2. **Configuration**:
   ```ini
   [global]
   jwt-secret = ${JWT_SECRET}  # Minimum 32 characters
   jwt-expiry-hours = 24
   ```

### Invalid or Expired Token

**Symptoms**:
```
HTTP 401 Unauthorized
Error: Invalid or expired token
```

**Diagnosis**:
```bash
# Check token expiry
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/jobs
```

**Solutions**:

1. **Token expired**:
   ```bash
   # Generate new token
   curl -X POST http://localhost:8080/api/login \
     -H "Content-Type: application/json" \
     -d '{"username":"admin","password":"your-password"}'
   ```

2. **Token refresh**:
   ```bash
   # Refresh existing token
   curl -X POST http://localhost:8080/api/refresh \
     -H "Authorization: Bearer $OLD_TOKEN"
   ```

3. **Increase token expiry**:
   ```ini
   [global]
   jwt-expiry-hours = 168  # 1 week instead of 24 hours
   ```

### Too Many Login Attempts

**Symptoms**:
```
HTTP 429 Too Many Requests
Error: Too many authentication attempts
```

**Diagnosis**:
```bash
# Check rate limit status
curl -I http://localhost:8080/api/login
```

**Solutions**:

1. **Wait for rate limit reset** (default: 1 minute)

2. **Adjust rate limit**:
   ```go
   // In configuration (if exposed)
   max-login-attempts = 10  # Increase from 5
   ```

3. **Check for brute force attack**:
   ```bash
   # Review authentication logs
   docker logs ofelia | grep "Authentication failed"
   ```

## Job Execution Issues

### Job Not Running

**Symptoms**:
- Job scheduled but never executes
- No execution history
- No errors in logs

**Diagnosis**:
```bash
# List all jobs
curl http://localhost:8080/api/jobs

# Check job status
curl http://localhost:8080/api/jobs/backup-db

# Check scheduler logs
docker logs ofelia | grep "backup-db"
```

**Solutions**:

1. **Schedule not reached yet**:
   ```bash
   # Manual trigger for testing
   curl -X POST http://localhost:8080/api/jobs/run \
     -H "Content-Type: application/json" \
     -d '{"name":"backup-db"}'
   ```

2. **Invalid schedule format**:
   ```ini
   # Check cron expression
   schedule = 0 */6 * * *  # Valid
   ```

3. **Job disabled**:
   ```bash
   # Enable job
   curl -X POST http://localhost:8080/api/jobs/enable \
     -d '{"name":"backup-db"}'
   ```

4. **Overlap prevention blocking execution**:
   ```ini
   # Previous job still running
   overlap = false  # Remove or set to true
   ```

### Jobs Being Skipped

**Symptoms**:
- Jobs run initially but start getting skipped after some time
- Log shows "skipping job - already running"
- Scheduled executions are missed without errors

**Root Cause**:

When `overlap = false` (or `no-overlap: 'true'` in Docker labels), Ofelia prevents concurrent executions of the same job. If a previous execution appears to still be running (e.g., a hung process that never terminated), subsequent scheduled runs will be skipped.

Common causes of "stuck" jobs:
- **Node.js**: Unhandled promise rejections don't exit by default
- **Python**: Background threads keeping process alive
- **Shell scripts**: Backgrounded processes or orphaned subprocesses
- **Deadlocks**: Application waiting for resources indefinitely

**Diagnosis**:
```bash
# Check if previous job is still "running"
docker logs ofelia 2>&1 | grep -i "already running\|skipping"

# Check container processes
docker exec <container> ps aux

# Check for zombie processes
docker exec <container> ps aux | grep defunct
```

**Solutions**:

1. **Ensure proper process exit for Node.js**:
   ```bash
   # Force exit on unhandled rejections
   node --unhandled-rejections=strict index.js
   ```

2. **Add explicit exit handling**:
   ```javascript
   // Node.js
   process.on('uncaughtException', (err) => {
     console.error('Uncaught exception:', err);
     process.exit(1);
   });
   ```

   ```python
   # Python
   import sys
   import atexit
   atexit.register(lambda: sys.exit(0))
   ```

3. **Set command timeout**:
   ```ini
   [job-exec "my-job"]
   schedule = @hourly
   container = app
   command = timeout 300 ./my-script.sh  # 5-minute timeout
   ```

4. **Temporarily disable overlap protection**:
   ```ini
   # For debugging - allows concurrent runs
   overlap = true
   ```

5. **Use shell wrapper with timeout**:
   ```bash
   #!/bin/bash
   set -e  # Exit on error
   timeout 600 ./actual-command.sh || exit $?
   ```

**Prevention**:
- Always use `set -e` in shell scripts
- Implement proper signal handling in long-running processes
- Add timeouts to external API calls and database queries
- Use process supervisors for complex jobs

### Job Execution Fails

**Symptoms**:
```
Error: Job execution failed with exit code 1
Error: Command not found
```

**Diagnosis**:
```bash
# Get job history
curl http://localhost:8080/api/jobs/backup-db/history

# Check execution logs
docker logs ofelia | grep "backup-db"
```

**Solutions**:

1. **Command not found**:
   ```ini
   # Use absolute path
   command = /usr/local/bin/backup.sh  # Not: backup.sh

   # Or ensure PATH is set
   environment = PATH=/usr/local/bin:/usr/bin:/bin
   ```

2. **Permission denied**:
   ```ini
   # Run as correct user
   user = root  # Or user with permissions

   # Check file permissions
   # chmod +x /usr/local/bin/backup.sh
   ```

3. **Container not ready**:
   ```ini
   # Add delay before execution
   delay = 10s
   ```

4. **Script errors**:
   ```bash
   # Test script manually
   docker exec postgres /usr/local/bin/backup.sh

   # Check script logs
   docker exec postgres cat /var/log/backup.log
   ```

### Container Execution Timeout

**Symptoms**:
```
Error: Container execution timeout after 300s
Error: Container failed to respond
```

**Solutions**:

1. **Increase timeout**:
   ```ini
   [job-exec "long-task"]
   timeout = 600s  # 10 minutes
   ```

2. **Optimize task**:
   - Break into smaller sub-tasks
   - Improve script performance
   - Add progress logging

3. **Background execution**:
   ```bash
   # For very long tasks, run in background
   command = nohup long-task.sh &
   ```

## Resource Issues

### Memory Limit Exceeded

**Symptoms**:
```
Error: Container killed due to memory limit
Error: OOMKilled
```

**Diagnosis**:
```bash
# Check container stats
docker stats ofelia

# Check memory usage
docker inspect ofelia | grep Memory
```

**Solutions**:

1. **Increase memory limit**:
   ```yaml
   services:
     ofelia:
       deploy:
         resources:
           limits:
             memory: 1G  # Increase from 512M
   ```

2. **Optimize memory usage**:
   ```ini
   # Limit concurrent jobs
   max-concurrent-jobs = 3

   # Clean up after execution
   delete = true
   ```

3. **Monitor memory usage**:
   ```bash
   # Real-time monitoring
   watch docker stats ofelia
   ```

### System Resources Degraded

**Symptoms**:
```json
{
  "status": "degraded",
  "checks": {
    "system": {
      "status": "degraded",
      "message": "Memory usage >75%"
    }
  }
}
```

**Solutions**:

1. **Check resource usage**:
   ```bash
   # System memory
   free -h

   # Container resources
   docker stats
   ```

2. **Cleanup**:
   ```bash
   # Remove stopped containers
   docker container prune -f

   # Remove unused images
   docker image prune -a -f

   # Remove unused volumes
   docker volume prune -f
   ```

3. **Adjust limits**:
   ```yaml
   services:
     ofelia:
       deploy:
         resources:
           limits:
             cpus: '2'
             memory: 2G
           reservations:
             cpus: '1'
             memory: 512M
   ```

## Middleware Issues

### Email Notifications Not Working

**Symptoms**:
```
Error: Mail error: dial tcp: lookup smtp.gmail.com: no such host
Error: 535 Authentication failed
```

**Diagnosis**:
```bash
# Test SMTP connectivity
telnet smtp.gmail.com 587

# Check DNS resolution
nslookup smtp.gmail.com
```

**Solutions**:

1. **Network connectivity**:
   ```bash
   # Test from container
   docker exec ofelia ping smtp.gmail.com

   # Check firewall rules
   sudo iptables -L | grep 587
   ```

2. **Authentication failed**:
   ```ini
   [global]
   smtp-user = your-email@gmail.com
   smtp-password = ${SMTP_PASSWORD}

   # For Gmail: use App Password, not account password
   # Generate at: https://myaccount.google.com/apppasswords
   ```

3. **TLS/SSL issues**:
   ```ini
   # Skip TLS verification (not recommended for production)
   smtp-tls-skip-verify = true

   # Or use proper TLS
   smtp-port = 587  # STARTTLS
   # Or
   smtp-port = 465  # SSL/TLS
   ```

4. **Email address validation**:
   ```ini
   # Ensure valid email format
   email-to = admin@example.com, ops@example.com
   email-from = ofelia@example.com
   ```

### Slack Notifications Not Working

**Symptoms**:
```
Error: Slack webhook URL is invalid
Error: Post request timeout
```

**Diagnosis**:
```bash
# Test webhook manually
curl -X POST $SLACK_WEBHOOK \
  -H "Content-Type: application/json" \
  -d '{"text":"Test message"}'
```

**Solutions**:

1. **Invalid webhook URL**:
   ```ini
   # Ensure full URL with https://
   slack-webhook = https://hooks.slack.com/services/XXX/YYY/ZZZ
   ```

2. **Network timeout**:
   ```bash
   # Test connectivity
   docker exec ofelia curl -I https://hooks.slack.com

   # Check proxy settings if needed
   ```

3. **Webhook expired or revoked**:
   - Generate new webhook in Slack settings
   - Update configuration with new URL

### Output Saving Issues

**Symptoms**:
```
Error: invalid save folder: path traversal detected
Error: permission denied
```

**Solutions**:

1. **Path traversal attempt**:
   ```ini
   # Use absolute path without .. or ~
   save-folder = /var/log/ofelia  # Correct
   # Not: save-folder = ../../etc
   ```

2. **Permission denied**:
   ```bash
   # Create directory with proper permissions
   sudo mkdir -p /var/log/ofelia
   sudo chown 1000:1000 /var/log/ofelia
   sudo chmod 755 /var/log/ofelia
   ```

3. **Volume not mounted**:
   ```yaml
   services:
     ofelia:
       volumes:
         - ./logs:/var/log/ofelia
   ```

## Web UI Issues

### Cannot Access Web UI

**Symptoms**:
- `ERR_CONNECTION_REFUSED`
- `Connection timeout`

**Diagnosis**:
```bash
# Check if server is running
curl http://localhost:8080/health

# Check port binding
netstat -tulpn | grep 8080

# Check container ports
docker port ofelia
```

**Solutions**:

1. **Web UI not enabled**:
   ```ini
   [global]
   enable-web = true
   web-address = :8080
   ```

2. **Port conflict**:
   ```bash
   # Check what's using port 8080
   sudo lsof -i :8080

   # Use different port
   web-address = :9090
   ```

3. **Firewall blocking**:
   ```bash
   # Allow port through firewall
   sudo ufw allow 8080/tcp

   # Or iptables
   sudo iptables -A INPUT -p tcp --dport 8080 -j ACCEPT
   ```

4. **Docker port mapping**:
   ```yaml
   services:
     ofelia:
       ports:
         - "8080:8080"  # host:container
   ```

### Rate Limit Exceeded

**Symptoms**:
```
HTTP 429 Too Many Requests
Error: Rate limit exceeded
```

**Solutions**:

1. **Reduce request rate**
2. **Wait for rate limit window to reset** (default: 1 minute)
3. **Increase rate limit** (if configurable):
   ```go
   // Default: 100 requests/minute per IP
   // Adjust in server configuration if exposed
   ```

## Health Check Issues

### Docker Check Unhealthy

**Symptoms**:
```json
{
  "checks": {
    "docker": {
      "status": "unhealthy",
      "message": "Docker daemon not accessible"
    }
  }
}
```

**Solutions**:

1. **Docker daemon not running**:
   ```bash
   sudo systemctl start docker
   ```

2. **Docker socket permission**:
   ```bash
   sudo chmod 666 /var/run/docker.sock
   ```

3. **Wrong Docker host**:
   ```ini
   [global]
   docker-host = unix:///var/run/docker.sock
   ```

### Scheduler Check Unhealthy

**Symptoms**:
```json
{
  "checks": {
    "scheduler": {
      "status": "unhealthy",
      "message": "Scheduler not operational"
    }
  }
}
```

**Solutions**:

1. **Scheduler not started**:
   ```bash
   # Restart Ofelia
   docker restart ofelia
   ```

2. **Configuration error**:
   ```bash
   # Validate configuration
   ofelia validate --config=/etc/ofelia/config.ini
   ```

## Network Issues

### DNS Resolution Failures

**Symptoms**:
```
Error: lookup smtp.gmail.com: no such host
Error: dial tcp: lookup registry-1.docker.io: Temporary failure in name resolution
```

**Solutions**:

1. **Container DNS configuration**:
   ```yaml
   services:
     ofelia:
       dns:
         - 8.8.8.8
         - 8.8.4.4
   ```

2. **Host DNS issues**:
   ```bash
   # Test DNS from host
   nslookup smtp.gmail.com

   # Check /etc/resolv.conf
   cat /etc/resolv.conf
   ```

3. **Docker daemon DNS**:
   ```json
   // /etc/docker/daemon.json
   {
     "dns": ["8.8.8.8", "8.8.4.4"]
   }
   ```

### Network Connectivity Issues

**Symptoms**:
```
Error: dial tcp: i/o timeout
Error: no route to host
```

**Solutions**:

1. **Check network connectivity**:
   ```bash
   # From container
   docker exec ofelia ping google.com

   # From host
   ping google.com
   ```

2. **Docker network inspection**:
   ```bash
   # List networks
   docker network ls

   # Inspect network
   docker network inspect bridge
   ```

3. **Firewall rules**:
   ```bash
   # Check iptables
   sudo iptables -L

   # Allow Docker networks
   sudo iptables -A FORWARD -i docker0 -j ACCEPT
   ```

## Performance Issues

### Slow Job Execution

**Symptoms**:
- Jobs take longer than expected
- High CPU/memory usage

**Diagnosis**:
```bash
# Monitor resource usage
docker stats ofelia

# Check job execution time
curl http://localhost:8080/api/jobs/slow-job/history

# Profile application
# Enable pprof if configured
curl http://localhost:6060/debug/pprof/profile?seconds=30 > profile.out
```

**Solutions**:

1. **Resource constraints**:
   ```yaml
   # Increase limits
   services:
     ofelia:
       deploy:
         resources:
           limits:
             cpus: '2'
             memory: 2G
   ```

2. **Optimize jobs**:
   - Reduce concurrent jobs
   - Add job priorities
   - Optimize scripts

3. **Container overhead**:
   ```ini
   # For LocalJobs (if allowed)
   # Bypass container overhead
   [job-local "fast-task"]
   command = /usr/local/bin/fast-script.sh
   ```

### High Memory Usage

**Symptoms**:
```
Memory usage consistently >90%
OOMKilled errors
```

**Solutions**:

1. **Memory leak investigation**:
   ```bash
   # Enable pprof
   enable-pprof = true
   pprof-address = :6060

   # Capture heap profile
   curl http://localhost:6060/debug/pprof/heap > heap.out
   go tool pprof heap.out
   ```

2. **Limit concurrent execution**:
   ```ini
   # Reduce parallel jobs
   max-concurrent-jobs = 5
   ```

3. **Monitor and alert**:
   ```bash
   # Set up Prometheus alerts
   # Alert when memory usage >80% for 5 minutes
   ```

## Debugging Tips

### Enable Debug Logging

```ini
[global]
log-level = debug
```

Or via environment:
```bash
export OFELIA_LOG_LEVEL=debug
docker restart ofelia
```

### Capture Debug Information

```bash
#!/bin/bash
# debug-info.sh - Collect debugging information

echo "=== Ofelia Version ==="
docker exec ofelia ofelia version

echo "=== Docker Info ==="
docker info

echo "=== Container Status ==="
docker ps -a | grep ofelia

echo "=== Container Logs (last 100 lines) ==="
docker logs --tail 100 ofelia

echo "=== Container Stats ==="
docker stats --no-stream ofelia

echo "=== Health Check ==="
curl -s http://localhost:8080/health | jq .

echo "=== Jobs Status ==="
curl -s http://localhost:8080/api/jobs | jq .

echo "=== Configuration ==="
docker exec ofelia cat /etc/ofelia/config.ini

echo "=== System Resources ==="
free -h
df -h
```

### Profiling

```bash
# Enable profiling
[global]
enable-pprof = true
pprof-address = :6060

# Capture profiles
curl http://localhost:6060/debug/pprof/goroutine > goroutine.out
curl http://localhost:6060/debug/pprof/heap > heap.out
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.out

# Analyze
go tool pprof heap.out
```

## Getting Help

### Before Reporting Issues

1. ✅ Check this troubleshooting guide
2. ✅ Review logs with debug level enabled
3. ✅ Validate configuration
4. ✅ Test with minimal configuration
5. ✅ Collect debug information
6. ✅ Search existing GitHub issues

### Reporting Issues

Include the following information:

1. **Environment**:
   - Ofelia version: `ofelia version`
   - Docker version: `docker version`
   - OS: `uname -a`

2. **Configuration**: Sanitized config file (remove secrets)

3. **Logs**: Relevant log snippets with debug enabled

4. **Reproduction Steps**: Minimal example to reproduce

5. **Expected vs Actual**: What you expected vs what happened

### Support Channels

- **GitHub Issues**: https://github.com/netresearch/ofelia/issues
- **Documentation**: https://github.com/netresearch/ofelia/docs
- **Security Issues**: security@netresearch.de

## Related Documentation

- [Configuration Guide](./CONFIGURATION.md) - Configuration reference
- [Web Package](./packages/web.md) - API and authentication
- [Security Guide](./SECURITY.md) - Security best practices
- [PROJECT_INDEX](./PROJECT_INDEX.md) - Overall system architecture

---
*For urgent security issues, contact: security@netresearch.de*
