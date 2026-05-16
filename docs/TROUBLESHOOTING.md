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

### DOCKER_HOST with TCP Socket Proxy (v0.12.0 – v0.24.0)

**Symptoms**:

```
SDK provider failed to connect to Docker: pinging docker: Cannot connect to the Docker daemon at tcp://<host>:2375. Is the docker daemon running?
```

The error names the configured `DOCKER_HOST`, but the connection never actually reaches it.

**Affected Versions**: v0.12.0 – v0.24.0 (fixed in v0.24.1+).

**Affected Configurations**:

- `DOCKER_HOST=tcp://…` pointing at a Docker socket proxy (e.g. [tecnativa/docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy)).
- `DOCKER_HOST=tcp://…` pointing at a remote Docker daemon over plain TCP.
- Not affected: `DOCKER_HOST=unix://…` and the default `unix:///var/run/docker.sock`.

**Root Cause**:
The custom HTTP transport built inside the Docker SDK adapter chose its dialer from `ClientConfig.Host` only, which was empty in the production path. It therefore fell back to a Unix-socket dialer pinned to `/var/run/docker.sock`, even though the SDK itself correctly read `DOCKER_HOST` and pointed at the TCP endpoint. Every request was silently routed to a non-existent Unix socket, producing the misleading `Cannot connect to the Docker daemon at tcp://…` error.

**Solutions**:

1. **Upgrade to v0.24.1+** (recommended).
2. **Workaround for older versions**: bind-mount `/var/run/docker.sock` directly into the container instead of using a TCP socket proxy. This loses the proxy's security restrictions.

**Working example after the fix** (Docker Compose with `tecnativa/docker-socket-proxy`):

```yaml
services:
  ofelia:
    image: ghcr.io/netresearch/ofelia:0.24.1
    environment:
      DOCKER_HOST: tcp://ofelia-socket-proxy:2375
    networks: [ofelia-socket-proxy]
    depends_on:
      ofelia-socket-proxy:
        condition: service_healthy

  ofelia-socket-proxy:
    image: ghcr.io/tecnativa/docker-socket-proxy:v0.4.2
    networks: [ofelia-socket-proxy]
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      CONTAINERS: 1
      POST: 1
      EXEC: 1
    healthcheck:
      test: wget -t1 -T4 -qO- http://127.0.0.1:2375/_ping | grep -q "OK" || exit 1

networks:
  ofelia-socket-proxy:
```

**References**:
- Issue: [#605](https://github.com/netresearch/ofelia/issues/605)
- Fix: [#606](https://github.com/netresearch/ofelia/pull/606)

### `run_exec` / exec-attach fails with `tls: first record does not look like a TLS handshake` (v0.25.0 only)

**Symptoms**:

```
[Job "..." (...)] Finished in "...", failed: true, error: job run: exec run:
run_exec container "...": cannot connect to the Docker daemon. Is 'docker daemon'
running on this host?: tls: first record does not look like a TLS handshake
```

Container discovery succeeds (Ofelia logs the job as registered and started); only the `run_exec` execution fails. Same root cause affects `ContainerAttach` and `ContainerLogs --follow` if they're ever exercised over the same transport.

**Affected Versions**: v0.25.0 only (fixed in v0.25.1+).

**Affected Configurations**:

- `DOCKER_HOST=tcp://...` (plain HTTP, no TLS material) — typically routed through `tecnativa/docker-socket-proxy` or another plain-HTTP socket proxy.
- Not affected: `unix://`, `tcp+tls://`, `https://`, and `tcp://` + `DOCKER_CERT_PATH` (which auto-upgrades to `https://` per [#634](https://github.com/netresearch/ofelia/issues/634)).

**Root Cause**:

Go's `net/http` lazily auto-configures HTTP/2 on the first request through a transport. As part of that setup it *allocates* `*http.Transport.TLSClientConfig` in place (to seed `NextProtos=[h2 http/1.1]` for ALPN) — even when no TLS is in use. The Docker SDK's hijack-path dialer reads `baseTransport.TLSClientConfig != nil` as its "TLS is required" signal. So after the first ordinary HTTP request mutates the transport, every subsequent hijack call (`ContainerExecAttach` / `ContainerAttach` / `ContainerLogs --follow`) misroutes to `tls.Dial` against a plain-HTTP daemon and the handshake fails.

The regular HTTP path doesn't consult `cli.tlsConfig()` and is unaffected — which is why container discovery worked but `run_exec` failed.

**Solutions**:

1. **Upgrade to v0.25.1+** (recommended). The fix sets `*http.Transport.TLSNextProto` to a non-nil empty map on the non-TLS apply paths — the documented stdlib opt-out for HTTP/2 auto-config — which keeps `TLSClientConfig` nil so the SDK's hijack dialer correctly falls to `net.Dial("tcp", …)`.
2. **No clean workaround for v0.25.0**. Switching to `unix://` (bind-mounting the socket directly) sidesteps the bug but defeats the security purpose of running a socket proxy. Operators who must stay on v0.25.0 should disable `run_exec` jobs or pin to v0.24.1 until the upgrade is feasible.

**References**:
- Issue: [#668](https://github.com/netresearch/ofelia/issues/668)
- Fix: [#681](https://github.com/netresearch/ofelia/pull/681)
- Related sibling fix (`http://` hijack): [#682](https://github.com/netresearch/ofelia/issues/682)

### `DOCKER_HOST=http://...` exec attach fails with `dial http: unknown network http`

**Symptoms**:

```
[Job "..." (...)] Finished in "...", failed: true, error: job run: exec run:
run_exec container "...": cannot connect to the Docker daemon. Is 'docker daemon'
running on this host?: dial http: unknown network http
```

Container discovery succeeds; only the hijack-using APIs fail (`run_exec` / `ContainerAttach` / `ContainerLogs --follow`).

**Affected Versions**: pre-v0.25.1 (sibling to the v0.25.0 `tcp://` regression above; the `http://` variant is older and existed before v0.25.0).

**Affected Configurations**:

- `DOCKER_HOST=http://...` (plain HTTP over TCP, not the canonical `tcp://...` form).
- Not affected: `unix://`, `tcp://`, `tcp+tls://`, `https://`.

**Root Cause**:

The Docker SDK's hijack-path dialer falls through to `net.Dial(cli.proto, cli.addr)`. For `proto == "http"` that becomes `net.Dial("http", addr)`, which Go's `net` package rejects — `"http"` is not a valid network name (only `"tcp"`, `"tcp4"`, `"tcp6"`, `"udp"`, `"unix"`, etc.). Distinct root cause from [#668](https://github.com/netresearch/ofelia/issues/668): that one was a runtime mutation triggering `tls.Dial`; this is a static "wrong network constant" failure.

**Solutions**:

1. **Upgrade to v0.25.1+** (recommended). The fix installs an explicit TCP `DialContext` on the `http://` transport so the SDK picks our dialer via `dialerFromTransport` instead of reaching the broken default.
2. **Workaround**: use `tcp://host:port` instead of `http://host:port`. Functionally identical for the Docker API; `tcp://` is the documented Docker CLI form.

**References**:
- Issue: [#682](https://github.com/netresearch/ofelia/issues/682)
- Fix: [#686](https://github.com/netresearch/ofelia/pull/686)

### Unsupported `DOCKER_HOST` Scheme

**Symptoms**:
```
Error: creating docker client: unsupported DOCKER_HOST scheme: "ssh://"; supported schemes: unix://, tcp://, tcp+tls://, http://, https://, npipe://
```

**Root Cause**:
Ofelia's Docker adapter validates the `DOCKER_HOST` URL scheme at startup
against an explicit allow-list. Earlier versions silently fell through to a
plain-TCP transport for unrecognized schemes (e.g. `ssh://`, `fd://`),
which produced opaque dial errors. `tcp+tls://` was historically in the
same boat but is now supported (see the table below — the TLS plumbing
landed in [#613](https://github.com/netresearch/ofelia/pull/613) and the
allow-list was re-opened in [#625](https://github.com/netresearch/ofelia/pull/625)).
See [#609](https://github.com/netresearch/ofelia/issues/609).

**Supported schemes** (case-insensitive — `TCP://` is normalized to `tcp://`):

| Scheme | Use case | Transport |
| --- | --- | --- |
| `unix://` | Default on Linux/macOS | Unix domain socket, HTTP/1.1 |
| `tcp://` | Plain TCP to a remote daemon (auto-upgrades to `https://` end-to-end when `DOCKER_CERT_PATH` is set, mirroring the docker CLI — see [#634](https://github.com/netresearch/ofelia/issues/634)) | TCP, HTTP/1.1 (TLS + HTTP/2 via ALPN when upgraded) |
| `tcp+tls://` | Explicit TLS over TCP (requires `DOCKER_CERT_PATH` / `DOCKER_TLS_VERIFY`) | TLS, HTTP/2 via ALPN |
| `http://` | Plain HTTP to a remote daemon | TCP, HTTP/1.1 |
| `https://` | HTTPS to a remote daemon | TLS, HTTP/2 via ALPN |
| `npipe://` | Windows named pipe (Windows builds only) | Named pipe, HTTP/1.1 |

**Unsupported schemes** (rejected at startup):

- `ssh://` — Docker over SSH. Not wired up; use an SSH-forwarded socket
  instead and point `DOCKER_HOST` at the forwarded `unix://` path.
- `fd://` — systemd socket activation. Not applicable to Ofelia's startup
  model; bind Ofelia to the actual `unix://` socket path.
- Any other scheme (e.g. `gopher://`, `tcp+ssh://`).

**Solution**:
Set `DOCKER_HOST` (or the `--docker-host` flag / `docker-host` config key)
to a value using one of the supported schemes above.

### TLS Handshake / Cert Path Errors (v0.24.1+)

**Symptoms**:

```
Docker TLS config could not be loaded; falling back to default TLS without client cert / pinned CA
```

or, when the daemon enforces mTLS:

```
SDK provider failed to connect to Docker: pinging docker: ... x509: certificate signed by unknown authority
```

**Affected**: `DOCKER_HOST=https://...` or `DOCKER_HOST=tcp+tls://...` with `DOCKER_TLS_VERIFY=1` and `DOCKER_CERT_PATH=...` set.

**Root Cause** (pre-v0.24.1): Ofelia's custom HTTP client overwrote the SDK's TLS-configured transport, silently dialing without the configured client cert and pinned CA. Operators believing they had mTLS were getting unauthenticated connections. Fixed in v0.24.1 — Ofelia now reads `DOCKER_CERT_PATH` / `DOCKER_TLS_VERIFY` directly and applies the cert material to the transport.

**Resolution checklist**:

1. `DOCKER_CERT_PATH` must point at a directory containing all three files: `ca.pem`, `cert.pem`, `key.pem`. The Docker SDK does not synthesize any of them.
2. `key.pem` must be readable by the Ofelia process. Recommended mode: `0600`.
3. `DOCKER_TLS_VERIFY` semantics are quirky and inherited from the upstream SDK — **any non-empty value** (`"1"`, `"0"`, `"yes"`, `"no"`) enables verification. Only an unset / empty value disables it.
4. If you need to opt out of verification programmatically, set `ClientConfig.TLSVerify = ptr(false)`. The explicit config value always wins over the env var.

**References**:

- Issue: [#607](https://github.com/netresearch/ofelia/issues/607)
- Fix: [#613](https://github.com/netresearch/ofelia/pull/613)

### `tcp+tls://` Without Cert Material (fail-closed)

**Symptoms**:

```
Error: creating docker client: tcp+tls:// requires TLS material: set DOCKER_CERT_PATH/DOCKER_TLS_VERIFY or ClientConfig.TLSCertPath/TLSVerify; see docs/TROUBLESHOOTING.md
```

**Affected**: `DOCKER_HOST=tcp+tls://...` with **no** `DOCKER_CERT_PATH` /
`DOCKER_TLS_VERIFY` env vars and **no** `ClientConfig.TLSCertPath` /
`TLSVerify` overrides.

**Root Cause**: `tcp+tls://` is an *explicit* TLS opt-in scheme (versus
`tcp://`, which is ambiguous and may rely on stdlib defaults). Without cert
material, `resolveTLSConfig` would return `(nil, nil)` and the SDK would
silently dial TLS using Go's stdlib defaults — system CA bundle, **no**
client certificate. Operators believing they had mTLS would be getting
unauthenticated TLS handshakes against any daemon that doesn't strictly
require client auth. Ofelia now fails the construction at startup so the
misconfiguration is loud rather than silent at runtime.

This is the analog, for `tcp+tls://`, of the silent plain-TCP downgrade
closed by [#612](https://github.com/netresearch/ofelia/pull/612) /
[#625](https://github.com/netresearch/ofelia/pull/625).

**Resolution**: pick one of the supported sources of cert material; the
client must find readable `ca.pem`, `cert.pem`, `key.pem` at the path:

1. Environment (matches the standard `docker` CLI workflow):
   ```bash
   export DOCKER_CERT_PATH=/etc/docker/certs.d/myhost
   export DOCKER_TLS_VERIFY=1
   ```
2. Programmatic / test override:
   ```go
   NewClientWithConfig(&ClientConfig{
       Host:        "tcp+tls://daemon:2376",
       TLSCertPath: "/etc/docker/certs.d/myhost",
   })
   ```
3. If you genuinely want plain TLS without mTLS (rare — almost always a
   misconfiguration) switch to `https://`, which keeps the upstream SDK's
   documented fail-open-with-warning posture.

`tcp://` and `https://` are unaffected and remain fail-open.

**References**:

- Issue: [#627](https://github.com/netresearch/ofelia/issues/627)
- Trigger: [#625](https://github.com/netresearch/ofelia/pull/625) (re-enabled `tcp+tls://`)

### Docker Daemon Wedged (startup or health-check fails fast with `context deadline exceeded`)

**Symptoms** (any of):

```
SDK provider failed to connect to Docker: pinging docker: context deadline exceeded
```

```
Docker API version negotiation timed out; continuing with default API version
```

`/health` returns non-2xx within ~5 seconds; `/ready` reports `unhealthy` for the `docker` check.

`ofelia doctor` reports each Docker call individually (Ping ~5s, `HasImageLocally` ~5s per image).

**Affected**: any setup where the Docker socket / TCP endpoint is *reachable* (TCP handshake succeeds, unix socket connects) but the daemon itself does not respond in time. The most common cause is a Docker socket proxy whose upstream daemon is wedged or under load.

**Root Cause**: every Docker SDK call from Ofelia is now bounded by an explicit timeout (#608/#611 for `NegotiateAPIVersion`, #614/#636 for the rest). What used to be a silent indefinite hang is now a fast loud failure with `context deadline exceeded`. This is a behavior improvement — the previous indefinite hang made monitoring blind to wedged daemons.

**Resolution**:

1. Verify the daemon is actually responsive: `docker --host=$DOCKER_HOST info` from the same machine.
2. If you use a socket proxy (tecnativa, etc.), check the proxy's upstream-daemon health. Restart the proxy if its connection to `/var/run/docker.sock` is stale.
3. Check `docker info` server-side latency. If the daemon takes more than ~5 seconds to respond to a Ping under normal load, file an issue — the bound is operator-tunable in source but not currently exposed via config.

The bounds are: 5s for `/health` and `/ready`, 10s for the startup sanity Pings, 5s per call for `ofelia doctor`, and 30s for the initial API version negotiation.

**References**:

- Issue: [#608](https://github.com/netresearch/ofelia/issues/608), [#614](https://github.com/netresearch/ofelia/issues/614)
- Fix: [#611](https://github.com/netresearch/ofelia/pull/611), [#636](https://github.com/netresearch/ofelia/pull/636)

### HTTP/2 Protocol Errors (v0.11.0 Only)

> **Different bug:** if you're on **v0.25.0** and seeing `tls: first record does not look like a TLS handshake` from `run_exec` jobs, that's a separate HTTP/2-related regression — see [`run_exec` / exec-attach fails with `tls: first record does not look like a TLS handshake`](#run_exec--exec-attach-fails-with-tls-first-record-does-not-look-like-a-tls-handshake-v0250-only) above.

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
       image: ghcr.io/netresearch/ofelia:latest
   ```

2. **Downgrade to v0.10.2** (Temporary workaround):
   ```yaml
   services:
     ofelia:
       image: ghcr.io/netresearch/ofelia:v0.10.2
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
| `job-service-run` | Ofelia container | Creates Swarm services, not tied to existing containers |

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
    image: ghcr.io/netresearch/ofelia:latest
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

   See [SMTP TLS verification trade-off](#smtp-tls-verification-trade-off-smtp-tls-skip-verify)
   below before enabling `smtp-tls-skip-verify` in production.

4. **Email address validation**:
   ```ini
   # Ensure valid email format
   email-to = admin@example.com, ops@example.com
   email-from = ofelia@example.com
   ```

### SMTP TLS verification trade-off (`smtp-tls-skip-verify`)

**Symptoms** (TLS verification failing against the SMTP relay):
```
Error: Mail error: x509: certificate signed by unknown authority
Error: Mail error: x509: certificate is valid for ..., not smtp.internal
Error: Mail error: tls: failed to verify certificate
```

**What `smtp-tls-skip-verify = true` does**:
- Sets `tls.Config.InsecureSkipVerify = true` on the SMTP dialer.
- The TLS handshake still happens (the wire is encrypted), but the server's
  certificate chain is **not** validated against any CA, and the certificate's
  Subject/SAN is **not** matched against the configured `smtp-host`.
- A network-position attacker (compromised router, hostile WiFi, malicious DNS
  resolver) can present any certificate and Ofelia will still send the message —
  including the SMTP credentials in `smtp-user` / `smtp-password`.

**When it's appropriate**:
- **Test or development environments** where the SMTP relay uses a self-signed
  certificate and rotating trusted CAs is not worth the operational cost.
- **Internal SMTP relays signed by a private CA** that you cannot install into
  the Ofelia container's system trust store. Prefer mounting the CA bundle into
  `/etc/ssl/certs/` (or building a custom image with the CA installed) over
  disabling verification entirely.
- **Legacy mail servers** that present an expired or hostname-mismatched
  certificate which you do not control. This should be a temporary workaround,
  not a long-term posture.

**When it's NOT appropriate**:
- Public SMTP relays (Gmail, SendGrid, Mailgun, AWS SES, Office 365). These
  always present valid public-CA certificates — a verification failure here is a
  real signal (DNS hijack, MITM, expired CA bundle in the container) and should
  not be silenced.
- Any environment where the SMTP credentials grant write access to a mailbox
  that other systems trust (alerting pipelines, Jira-by-email integrations,
  etc.). Leaking those credentials via a MITM converts a notification channel
  into an exploitation vector.

**Recommended alternatives** (in preference order):
1. Fix the certificate — renew it, add the correct SAN, use Let's Encrypt.
2. Install the private CA in the container's trust store
   (mount `ca-bundle.crt` into `/etc/ssl/certs/` and run `update-ca-certificates`
   in a custom image).
3. Use a localhost SMTP relay (Postfix sidecar) that handles upstream TLS for
   you, and let Ofelia connect to `127.0.0.1:25` over plain TCP.
4. Only as a last resort: `smtp-tls-skip-verify = true`, scoped to environments
   where the threat model accepts the risk.

```ini
[global]
smtp-host = smtp.internal.example.com
smtp-port = 587
smtp-user = ofelia@internal
# WARNING: smtp-user / smtp-password are exposed to ANY MITM on this path
# whenever skip-verify is enabled. Ensure the network segment is fully
# trusted before reusing this credential anywhere it could be replayed.
smtp-password = ${SMTP_PASSWORD}
# Internal SMTP server uses a private CA we can't easily distribute.
# Acceptable here because the path is fully inside our VPC.
smtp-tls-skip-verify = true
```

### SMTP STARTTLS posture (`smtp-tls-policy`)

**Symptoms**:
```
Error: Mail error: STARTTLS is required, but the server doesn't support it
Error: Mail error: 502 5.5.1 STARTTLS not advertised
```
or — if you upgraded from an Ofelia version older than the `smtp-tls-policy` introduction —
mail used to send fine and now fails with one of the messages above.

**What changed**: The default STARTTLS posture is now `mandatory` (was `opportunistic`).
The previous default would silently send credentials and message body in cleartext when
the server did not advertise STARTTLS, which violated the operator's intent. Tracked in
[#653](https://github.com/netresearch/ofelia/issues/653).

**Valid `smtp-tls-policy` values**:

- `mandatory` (default) — Require STARTTLS; the dialer aborts with an error if the
  server does not advertise it. This is the upstream go-mail recommendation for any
  modern SMTP server (Gmail, SendGrid, Mailgun, AWS SES, Office 365, Postfix on
  port 587).
- `opportunistic` — Try STARTTLS when offered; silently fall back to plaintext
  otherwise. Use **only** when the network path is fully trusted (loopback / sidecar
  relay) and the upstream cannot offer STARTTLS.
- `none` — Disable STARTTLS entirely; messages and credentials are sent in
  cleartext. Required for some test fixtures (MailHog, `emersion/go-smtp` without
  TLS). Intentionally insecure — never set in production.

**Migration recipes**:

```ini
# Modern relay (Gmail / SendGrid / Mailgun / Postfix on 587):
[global]
smtp-host = smtp.gmail.com
smtp-port = 587
# smtp-tls-policy = mandatory  # default; can omit
```

```ini
# Localhost MTA sidecar that doesn't speak STARTTLS but is on a trusted
# loopback path:
[global]
smtp-host = 127.0.0.1
smtp-port = 25
smtp-tls-policy = opportunistic
```

```ini
# Test environment using MailHog or an in-cluster fake SMTP without TLS:
[global]
smtp-host = mailhog.test.svc
smtp-port = 1025
smtp-tls-policy = none
```

An unknown value (typo such as `smtp-tls-policy = required`) is normalized to
`mandatory` at runtime and a `WARN`-level log line is emitted. We refuse to
silently weaken transport security based on a typo.

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

### Webhook Notifications Not Working

**Symptoms**:
- Webhooks not firing after job execution
- No webhook-related log messages
- "Unknown job type webhook" warnings in logs

**Diagnosis**:
```bash
# Check logs for webhook-related messages
docker logs ofelia 2>&1 | grep -i webhook

# Verify webhook labels are on the service container
docker inspect --format '{{json .Config.Labels}}' ofelia-service | jq 'to_entries[] | select(.key | startswith("ofelia.webhook"))'
```

**Solutions**:

1. **Webhook labels on wrong container**: Webhook labels (`ofelia.webhook.*`) are only processed from the **service container** (with `ofelia.service=true`). Move webhook labels to the Ofelia service container:
   ```yaml
   ofelia:
     labels:
       ofelia.service: "true"
       ofelia.enabled: "true"
       ofelia.webhook.slack.preset: slack
       ofelia.webhook.slack.id: "T00/B00"
       ofelia.webhook.slack.secret: "secret"
   ```

2. **Missing webhook assignment on job**: Define which webhooks a job should use:
   ```yaml
   worker:
     labels:
       ofelia.job-exec.backup.webhooks: "slack"
   ```

3. **INI webhook overriding label webhook**: If a webhook with the same name exists in both the INI file and Docker labels, the INI version takes precedence. Use a different name for the label-defined webhook or remove the INI definition.

4. **Trigger mismatch**: Verify the webhook's `trigger` matches the job outcome:
   - `error` — only fires when the job fails
   - `success` — only fires when the job succeeds
   - `always` — fires on every execution

5. **Only the first webhook in the list fires (≤ v0.25.0)**: Fixed in the
   next release. Versions up to and including v0.25.0 silently dropped every
   webhook after the first when a job listed multiple (e.g.
   `webhooks: "wh-success, wh-error"`), and dropped the global selector
   entirely for jobs that declared their own `webhooks:`. The dedup happened
   in the middleware container by reflect type, so there was no log signal.
   Workaround on affected versions: list the most important webhook FIRST
   in the `webhooks:` string. Permanent fix: upgrade to the next release
   ([#670](https://github.com/netresearch/ofelia/issues/670)). After upgrade,
   webhook-attach failures (unknown name, preset-load failure, missing
   required variable) are logged at `ERROR` level keyed by job name —
   `docker logs ofelia 2>&1 | grep "webhook middleware attach failed"`.

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
