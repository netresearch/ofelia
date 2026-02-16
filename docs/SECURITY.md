# Security Considerations

**Last Updated**: 2025-12-17
**Security Review Date**: 2025-12-17

## Overview

Ofelia implements defense-in-depth security practices across authentication, input validation, and application stability. This document covers security features, best practices, and deployment considerations for production environments.

## Security Responsibility Model

Understanding what security controls belong where is critical for proper deployment:

```
┌─────────────────────────────────────────────────────────────────────┐
│                 INFRASTRUCTURE RESPONSIBILITY                        │
│  (Docker daemon, Kubernetes, host OS, network)                       │
│                                                                      │
│  • Container privileges (--privileged, capabilities)                 │
│  • Host mounts and volume permissions                                │
│  • Network isolation and firewall rules                              │
│  • Resource limits (cgroups, ulimits)                                │
│  • Security profiles (AppArmor, SELinux, seccomp)                    │
│  • Docker socket access control                                      │
│  • TLS termination (reverse proxy)                                   │
└─────────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│                   OFELIA RESPONSIBILITY                              │
│  (Application-level controls)                                        │
│                                                                      │
│  • Authentication (tokens, passwords)                                │
│  • Authorization (who can create/run jobs) - Note: No RBAC yet       │
│  • Input format validation (cron syntax, image names)                │
│  • Rate limiting for API endpoints                                   │
│  • Session management and token handling                             │
│  • Application stability (memory bounds, graceful shutdown)          │
│  • Audit logging of security events                                  │
└─────────────────────────────────────────────────────────────────────┘
```

**Key Principle**: Ofelia schedules jobs as requested. The infrastructure enforces what's permitted. If you need to restrict container privileges, configure your Docker daemon, use rootless Docker, or deploy with Kubernetes PodSecurityStandards. See [ADR-002](./adr/ADR-002-security-boundaries.md) for the full rationale.

## Security Architecture

```
┌─────────────────────────────────────────────────┐
│         External Access Layer                   │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐      │
│  │   TLS    │  │   HTTPS  │  │  mTLS    │      │
│  └──────────┘  └──────────┘  └──────────┘      │
└─────────────────────────────────────────────────┘
                      ↓
┌─────────────────────────────────────────────────┐
│         Authentication Layer                     │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐      │
│  │  Token   │  │  Bcrypt  │  │   CSRF   │      │
│  └──────────┘  └──────────┘  └──────────┘      │
└─────────────────────────────────────────────────┘
                      ↓
┌─────────────────────────────────────────────────┐
│         Input Validation Layer                   │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐      │
│  │Sanitizer │  │Validator │  │  Filters │      │
│  └──────────┘  └──────────┘  └──────────┘      │
└─────────────────────────────────────────────────┘
                      ↓
┌─────────────────────────────────────────────────┐
│         Application Layer                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐      │
│  │  Jobs    │  │  API     │  │Scheduler │      │
│  └──────────┘  └──────────┘  └──────────┘      │
└─────────────────────────────────────────────────┘
                      ↓
┌─────────────────────────────────────────────────┐
│         Container Isolation Layer                │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐      │
│  │ Docker   │  │Resources │  │ Networks │      │
│  └──────────┘  └──────────┘  └──────────┘      │
└─────────────────────────────────────────────────┘
```

## OWASP Top 10 Coverage

### A01:2021 - Broken Access Control
**Protection**: Token-based authentication (single-user model)

- ✅ **Secure Authentication** ([web/auth_secure.go](../web/auth_secure.go)):
  - Bcrypt password hashing (cost factor 12)
  - Cryptographically secure token generation
  - Token expiry enforcement (configurable, default 24 hours)
  - Constant-time username comparison
  - Rate limiting per IP (default 5 attempts/minute)
  - Session management with secure cookies

- ⚠️ **Current Limitations**:
  - **No RBAC**: Single credential model - any authenticated user has full access
  - **No token revocation list**: Tokens valid until expiry (use short expiry in sensitive environments)
  - LocalJob restrictions only apply to Docker label sources

- ✅ **Access Control**:
  - API endpoints require valid token (when auth enabled)
  - LocalJob execution from labels restricted by default
  - Docker socket access delegated to infrastructure

**Configuration**:
```ini
[global]
web-auth-enabled = true
web-username = admin
web-password-hash = $2a$12$...  # bcrypt hash
web-secret-key = ${WEB_SECRET_KEY}
web-token-expiry = 24  # hours
allow-host-jobs-from-labels = false  # Restrict LocalJobs
```

### A02:2021 - Cryptographic Failures
**Protection**: Strong cryptography and secure password storage

- ✅ **Password Hashing**:
  ```go
  // Bcrypt with cost 12 (2^12 iterations)
  hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
  ```

- ✅ **Token Signing**:
  - HMAC-based token signing with minimum 32-byte secret
  - Automatic token key validation on startup

- ✅ **Secure Storage**:
  - Credentials never logged
  - Environment variable injection for secrets
  - Bcrypt hashing for production passwords

**Best Practices**:
```bash
# Generate bcrypt password hash
python3 -c "import bcrypt; print(bcrypt.hashpw(b'your-password', bcrypt.gensalt(12)).decode())"

# Generate secret key
openssl rand -base64 48

# Store in environment
export OFELIA_WEB_SECRET_KEY="your-generated-secret-here"
```

### A03:2021 - Injection Attacks
**Protection**: Multi-layer input validation and sanitization ([config/sanitizer.go](../config/sanitizer.go))

#### SQL Injection Prevention
```go
// Blocked patterns
union select, insert into, update set, delete from, drop table
create table, alter table, exec, execute
```

**Example**:
```go
sanitizer := config.NewSanitizer()
cleaned, err := sanitizer.SanitizeString(userInput, 1024)
// Removes: union select, <script>, javascript:, etc.
```

#### Shell Command Injection Prevention
```go
// Blocked operators
; & | < > $ ` && || >> << $( ${

// Blocked commands
rm -rf, dd if=, mkfs, format, sudo, su -
wget, curl, nc, telnet, chmod 777
```

**Example**:
```go
err := sanitizer.ValidateCommand("/backup/script.sh --dry-run")
// Blocks: shell operators, command substitution, dangerous commands
```

#### Path Traversal Prevention
```go
// Blocked patterns
../ ..\ ..%2F %2e%2e

// Blocked extensions
.exe .sh .dll .bat .cmd
```

**Example**:
```go
err := sanitizer.ValidatePath("/var/log/backup.log", "/var/log")
// Blocks: ../../../etc/passwd, URL-encoded traversal
```

#### Docker Image Injection Prevention
```go
// Validates format: [registry/]namespace/repository[:tag][@sha256:digest]
err := sanitizer.ValidateDockerImage("nginx:1.21-alpine")
// Blocks: invalid format, suspicious patterns (.., //)
```

### A04:2021 - Insecure Design
**Protection**: Security-first architecture

- ✅ **Defense in Depth**: Multiple security layers (auth, validation, isolation)
- ✅ **Fail-Safe Defaults**: Secure defaults, explicit opt-in for privileged operations
- ✅ **Least Privilege**: Minimal permissions, container isolation
- ✅ **Separation of Duties**: Job types enforce execution boundaries
- ✅ **Input Validation**: All inputs validated before processing

**Secure Design Patterns**:
```ini
# LocalJobs disabled by default from labels
allow-host-jobs-from-labels = false

# Containers deleted after execution
delete = true

# Overlap prevention for critical jobs
overlap = false
```

### A05:2021 - Security Misconfiguration
**Protection**: Secure defaults and configuration validation

- ✅ **Secure Defaults**:
  - Docker events enabled for real-time monitoring
  - Container cleanup enabled
  - HTTP security headers enforced
  - Rate limiting enabled

- ✅ **Configuration Validation** ([config/validator.go](../config/validator.go)):
  ```go
  validator := config.NewValidator()
  validator.ValidateRequired("web-secret-key", config.WebSecretKey)
  validator.ValidateCronExpression("schedule", job.Schedule)
  validator.ValidateEmail("email-to", config.EmailTo)
  ```

- ✅ **Security Headers** ([web/middleware.go](../web/middleware.go)):
  ```go
  X-Content-Type-Options: nosniff
  X-Frame-Options: DENY
  X-XSS-Protection: 1; mode=block
  Referrer-Policy: strict-origin-when-cross-origin
  Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'
  Strict-Transport-Security: max-age=31536000 (when HTTPS)
  ```
  
  ⚠️ **Note**: CSP allows `'unsafe-inline'` for scripts and styles to support the embedded web UI. For stricter CSP, deploy behind a reverse proxy with custom headers.

### A06:2021 - Vulnerable and Outdated Components
**Protection**: Dependency management and security updates

- ✅ **Go Dependency Management**:
  ```bash
  # Regular security updates
  go get -u ./...
  go mod tidy

  # Security scanning
  govulncheck ./...
  ```

- ✅ **Docker Image Security**:
  - Base images from official sources
  - Regular image updates
  - Minimal attack surface (Alpine Linux)
  - No unnecessary packages

**Security Update Schedule**:
- **Critical vulnerabilities**: Immediate patch
- **High severity**: Within 7 days
- **Medium/Low**: Next release cycle
- **Dependencies**: Monthly review

### A07:2021 - Identification and Authentication Failures
**Protection**: Robust authentication mechanisms

- ✅ **Authentication Protections**:
  - Authentication tokens with configurable expiry
  - CSRF tokens for state-changing operations
  - Rate limiting prevents brute force

- ✅ **Session Management** ([web/auth_secure.go](../web/auth_secure.go)):
  ```go
  // Secure cookies
  cookie := &http.Cookie{
      Name:     "auth_token",
      Value:    token,
      Path:     "/",
      HttpOnly: true,           // Prevent JavaScript access
      Secure:   true,           // HTTPS only
      SameSite: http.SameSiteStrictMode,  // CSRF protection
      MaxAge:   int(h.tokenManager.tokenExpiry.Seconds()),
  }
  ```

- ✅ **Timing Attack Protection**:
  ```go
  // Constant-time comparison
  usernameMatch := subtle.ConstantTimeCompare(
      []byte(credentials.Username),
      []byte(h.config.Username)
  ) == 1

  // Delay on authentication failure (100ms)
  time.Sleep(100 * time.Millisecond)
  ```

### A08:2021 - Software and Data Integrity Failures
**Protection**: Code signing and integrity verification

- ✅ **Container Integrity**:
  - SHA256 image digests supported
  - Image signature verification (optional)
  - Immutable tags avoided in production

- ✅ **Configuration Integrity**:
  - Configuration validation before loading
  - Hash-based change detection
  - Atomic configuration updates

**Best Practices**:
```ini
# Use SHA256 digests for production images
image = nginx@sha256:abc123...

# Enable image pull always for latest security patches
pull = always
```

### A09:2021 - Security Logging and Monitoring Failures
**Protection**: Comprehensive logging and monitoring

- ✅ **Structured Logging** (stdlib `log/slog`):
  - All authentication attempts logged
  - Failed login tracking
  - Command execution logging
  - Source location via `AddSource: true`

- ✅ **Security Event Logging**:
  ```go
  logger.WarnWithFields("Authentication failed", map[string]interface{}{
      "username": username,
      "ip": clientIP,
      "attempt": attemptCount,
  })
  ```

- ✅ **Prometheus Metrics** ([metrics/prometheus.go](../metrics/prometheus.go)):
  ```
  ofelia_http_requests_total{status="401"}  # Failed auth
  ofelia_jobs_failed_total                   # Failed jobs
  ofelia_docker_errors_total                 # Docker errors
  ```

### A10:2021 - Server-Side Request Forgery (SSRF)
**Protection**: Trust-the-config model with optional host whitelist

Ofelia follows a **trust-the-config** security model for webhooks: since users can already run arbitrary commands via local/exec jobs, the same trust level applies to webhook destinations. **All hosts are allowed by default.**

- ✅ **Trust Model** ([middlewares/webhook_security.go](../middlewares/webhook_security.go)):
  - If you control the configuration, you control the behavior
  - Same trust level as local command execution
  - Default: `webhook-allowed-hosts = *` (allow all hosts)

- ✅ **URL Validation**:
  - Only `http://` and `https://` schemes allowed
  - URL must have a valid hostname

- ✅ **Optional Whitelist Mode** (for multi-tenant/cloud deployments):
  - Set specific hosts to enable whitelist mode
  - Supports wildcards: `*.example.com`

**Default** (self-hosted/trusted environments):
```ini
[global]
# All hosts allowed by default (no config needed)
# webhook-allowed-hosts = *
```

**Whitelist Mode** (for cloud/multi-tenant deployments):
```ini
[global]
# Only allow specific hosts
webhook-allowed-hosts = hooks.slack.com, discord.com, ntfy.internal, 192.168.1.20
```

## Authentication & Authorization

### Token-Based Authentication

**Implementation**: [web/auth_secure.go](../web/auth_secure.go)

**Features**:
- Bcrypt password hashing (cost 12)
- Cryptographically secure token generation
- Constant-time username comparison
- Rate limiting (5 attempts/minute)
- CSRF token protection
- Timing attack prevention
- Secure HTTP-only cookies

**Password Hashing**:
```bash
# Generate bcrypt hash for configuration
python3 -c "import bcrypt; print(bcrypt.hashpw(b'your-password', bcrypt.gensalt(12)).decode())"
```

**Usage**:
```bash
# Login
curl -X POST http://localhost:8081/api/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"your-password"}'

# Response
{
  "token": "abc123...",
  "csrf_token": "xyz789...",
  "expires_in": 86400
}

# Use token
curl -H "Authorization: Bearer abc123..." \
  http://localhost:8081/api/jobs
```

**Rate Limiting**:
```go
// 5 attempts per minute per IP
rateLimiter := NewRateLimiter(5, 5)

if !rateLimiter.Allow(clientIP) {
    return errors.New("too many authentication attempts")
}
```

**CSRF Protection**:
```go
// Generate CSRF token (one-time use)
csrfToken, err := tokenManager.GenerateCSRFToken()

// Validate and consume token
valid := tokenManager.ValidateCSRFToken(token)
```

## Input Validation & Sanitization

### Comprehensive Validation

**Validator**: [config/validator.go](../config/validator.go)

**Validation Rules**:
- Required field validation
- String length validation (min/max)
- Email format validation
- URL format validation
- Cron expression validation
- Numeric range validation
- Path validation
- Enum validation

**Example**:
```go
validator := config.NewValidator()

// Required fields
validator.ValidateRequired("job-name", jobName)

// Email validation
validator.ValidateEmail("email-to", "admin@example.com")

// Cron expression validation
validator.ValidateCronExpression("schedule", "0 */6 * * *")

// URL validation
validator.ValidateURL("webhook-url", "https://example.com/webhook")

// Check for errors
if validator.HasErrors() {
    for _, err := range validator.Errors() {
        log.Printf("Validation error: %v", err)
    }
}
```

### Input Sanitization

**Sanitizer**: [config/sanitizer.go](../config/sanitizer.go)

**Attack Vectors Protected**:

| Attack Type | Protection Method | Blocked Patterns |
|------------|-------------------|------------------|
| SQL Injection | Pattern detection | `union select`, `insert into`, `drop table`, `<script>` |
| Shell Injection | Command validation | `; & \| < >`, `&&`, `\|\|`, `$()`, `` ` `` |
| Path Traversal | Path sanitization | `../`, `..\\`, `%2e%2e`, `~` |
| XSS | HTML escaping | `<`, `>`, `&`, `"`, `'` |
| SSRF | URL validation + optional whitelist | Scheme validation, optional host whitelist |
| LDAP Injection | Character filtering | `( ) * \| & !` |

**Usage Examples**:

```go
sanitizer := config.NewSanitizer()

// String sanitization (removes control chars, null bytes)
clean, err := sanitizer.SanitizeString(userInput, 1024)

// Command validation
err := sanitizer.ValidateCommand("/backup/script.sh --dry-run")

// Path validation with base path restriction
err := sanitizer.ValidatePath("/var/log/backup.log", "/var/log")

// Docker image validation
err := sanitizer.ValidateDockerImage("nginx:1.21-alpine")

// Environment variable validation
err := sanitizer.ValidateEnvironmentVar("MY_VAR", "value123")

// URL validation (scheme and format)
err := sanitizer.ValidateURL("https://api.example.com/webhook")
```

### Command Validation

**CommandValidator**: [config/command_validator.go](../config/command_validator.go)

**Features**:
- Service name validation (Docker)
- File path validation (sensitive directories blocked)
- Command argument sanitization
- Dangerous pattern detection
- Null byte injection prevention

**Blocked Patterns**:
```go
// Command substitution
$(...), `command`, ${var}

// Shell operators
; & | < > >> << &&  ||

// Directory traversal
../ ..\ %2e%2e

// Sensitive directories
/etc/, /proc/, /sys/, /dev/
```

**Example**:
```go
cmdValidator := config.NewCommandValidator()

// Validate service name
err := cmdValidator.ValidateServiceName("web-backend")

// Validate file path
err := cmdValidator.ValidateFilePath("/app/docker-compose.yml")

// Validate command arguments
args := []string{"--flag", "value", "/path/to/file"}
err := cmdValidator.ValidateCommandArgs(args)
```

## Docker Security

### Container Isolation

**Resource Limits**:
```ini
[job-run "isolated-job"]
image = myapp:latest
command = process-data

# Memory limits
memory = 512m
memory-swap = 1g

# CPU limits
cpu-shares = 512
cpu-quota = 50000
```

**Capabilities**:
```ini
# Drop dangerous capabilities
capabilities-drop = NET_RAW,SYS_ADMIN,SYS_MODULE

# Add only required capabilities
capabilities-add = NET_BIND_SERVICE
```

**User Restrictions**:
```ini
# Run as non-root user
user = 1000:1000

# Or specific username
user = appuser
```

### Network Security

**Network Isolation**:
```ini
# Isolated network
network = app_isolated

# No network access
network = none
```

**DNS Restrictions**:
```ini
# Internal DNS only
dns = 10.0.0.1,10.0.0.2
```

### Volume Security

**Read-Only Mounts**:
```ini
# Read-only data volume
volumes = /data:/data:ro

# Writable output only
volumes = /output:/output:rw
```

**Tmpfs for Sensitive Data**:
```ini
# Temporary in-memory storage
tmpfs = /tmp:rw,noexec,nosuid,size=100m
```

### Image Security

**Image Verification**:
```ini
# Use SHA256 digests
image = nginx@sha256:abc123...

# Pull policy
pull = always  # Always get latest security patches
```

**Trusted Registries**:
```ini
# Use only trusted registries
image = registry.example.com/myapp:1.0.0
```

### Docker Socket Security

**Socket Protection**:
```bash
# Restrict socket access
chmod 660 /var/run/docker.sock
chown root:docker /var/run/docker.sock

# Or use TCP with TLS
DOCKER_HOST=tcp://docker:2376
DOCKER_TLS_VERIFY=1
DOCKER_CERT_PATH=/certs
```

**LocalJob Restrictions**:
```ini
[global]
# Prevent LocalJobs from Docker labels
allow-host-jobs-from-labels = false
```

## Network Security

### TLS/HTTPS

**Production Deployment**:
```yaml
# Behind reverse proxy (recommended)
services:
  nginx:
    image: nginx:alpine
    ports:
      - "443:443"
    volumes:
      - ./ssl:/etc/nginx/ssl:ro
      - ./nginx.conf:/etc/nginx/nginx.conf:ro

  ofelia:
    image: netresearch/ofelia:latest
    expose:
      - "8080"
    environment:
      - OFELIA_WEB_ADDRESS=:8080
```

**NGINX Configuration**:
```nginx
server {
    listen 443 ssl http2;
    server_name ofelia.example.com;

    ssl_certificate /etc/nginx/ssl/cert.pem;
    ssl_certificate_key /etc/nginx/ssl/key.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    location / {
        proxy_pass http://ofelia:8080;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### Firewall Rules

**iptables Example**:
```bash
# Allow only necessary ports
iptables -A INPUT -p tcp --dport 443 -j ACCEPT
iptables -A INPUT -p tcp --dport 8080 -j DROP  # Block direct access

# Allow Docker network
iptables -A INPUT -i docker0 -j ACCEPT

# Rate limiting
iptables -A INPUT -p tcp --dport 443 -m state --state NEW \
  -m recent --set --name WEB
iptables -A INPUT -p tcp --dport 443 -m state --state NEW \
  -m recent --update --seconds 60 --hitcount 100 --name WEB -j DROP
```

### Rate Limiting

**Application-Level**:
```go
// HTTP rate limiting: 100 requests/minute per IP
rateLimiter := newRateLimiter(100, time.Minute)
```

**NGINX Rate Limiting**:
```nginx
limit_req_zone $binary_remote_addr zone=api:10m rate=10r/s;

location /api/ {
    limit_req zone=api burst=20 nodelay;
    proxy_pass http://ofelia:8080;
}
```

## Deployment Security

### Environment Variables

**Secret Management**:
```bash
# Use secret management (Docker Swarm)
docker secret create web_secret_key web_secret_key.txt
docker secret create smtp_password smtp_password.txt

# Reference in compose file
services:
  ofelia:
    secrets:
      - web_secret_key
      - smtp_password
    environment:
      - OFELIA_WEB_SECRET_KEY_FILE=/run/secrets/web_secret_key
      - OFELIA_SMTP_PASSWORD_FILE=/run/secrets/smtp_password
```

**Kubernetes Secrets**:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: ofelia-secrets
type: Opaque
data:
  web-secret-key: <base64-encoded>
  smtp-password: <base64-encoded>

---
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: ofelia
        env:
        - name: OFELIA_WEB_SECRET_KEY
          valueFrom:
            secretKeyRef:
              name: ofelia-secrets
              key: web-secret-key
```

### Least Privilege

**Docker Compose**:
```yaml
services:
  ofelia:
    image: netresearch/ofelia:latest
    user: "1000:1000"  # Non-root user
    cap_drop:
      - ALL
    cap_add:
      - NET_BIND_SERVICE  # Only if binding <1024
    read_only: true
    tmpfs:
      - /tmp:noexec,nosuid
```

### Security Scanning

**Container Scanning**:
```bash
# Trivy
trivy image netresearch/ofelia:latest

# Clair
docker run -p 6060:6060 -d --name clair clair
clairctl analyze netresearch/ofelia:latest
```

**Code Scanning**:
```bash
# Go vulnerability check
govulncheck ./...

# Static analysis
staticcheck ./...
gosec ./...
```

## Security Best Practices

### 1. Credential Management

- ✅ **Use environment variables for all secrets**
- ✅ **Never commit secrets to version control**
- ✅ **Rotate credentials regularly (90 days)**
- ✅ **Use secret management systems (Vault, Secrets Manager)**
- ✅ **Minimum password length: 12 characters**
- ✅ **Enforce password complexity requirements**

### 2. Access Control

- ✅ **Enable web authentication in production**
- ✅ **Use HTTPS/TLS for all external connections**
- ✅ **Implement IP whitelisting for API access**
- ✅ **Disable LocalJobs from Docker labels**
- ✅ **Use least privilege for container execution**
- ✅ **Regularly review and audit access logs**

### 3. Container Security

- ✅ **Run containers as non-root users**
- ✅ **Drop unnecessary capabilities**
- ✅ **Use read-only root filesystems**
- ✅ **Implement resource limits (CPU, memory)**
- ✅ **Scan images for vulnerabilities**
- ✅ **Use official base images only**
- ✅ **Keep images updated (automated patches)**

### 4. Network Security

- ✅ **Use isolated Docker networks**
- ✅ **Implement firewall rules**
- ✅ **Enable rate limiting**
- ✅ **Use reverse proxy with TLS**
- ✅ **Disable unnecessary ports**
- ✅ **Monitor network traffic**

### 5. Monitoring & Logging

- ✅ **Enable structured logging**
- ✅ **Monitor authentication failures**
- ✅ **Track failed job executions**
- ✅ **Set up alerting for security events**
- ✅ **Retain logs for 90 days minimum**
- ✅ **Implement centralized log aggregation**

### 6. Incident Response

- ✅ **Document incident response procedures**
- ✅ **Test backup and recovery regularly**
- ✅ **Maintain security contact information**
- ✅ **Have rollback procedures documented**
- ✅ **Conduct post-incident reviews**

## Security Updates (December 2025)

Recent security enhancements implemented:

### Web Authentication System (Dec 17, 2025)
- Secure token-based authentication wired up to web API
- Bcrypt password hashing (cost factor 12)
- Cryptographically secure token generation
- Configurable token expiry
- Auth middleware protects /api/* endpoints
- Dead auth code removed (legacy plain text auth, unused JWT handlers)

### Enhanced Password Security
- Bcrypt hashing (cost factor 12)
- Constant-time username comparison
- Timing attack protection
- 100ms delay on authentication failure

### CSRF Protection
- One-time use CSRF tokens
- Token validation middleware
- Secure cookie attributes
- SameSite cookie protection

### Rate Limiting
- Per-IP rate limiting (5 attempts/minute for auth)
- HTTP rate limiting (100 requests/minute)
- Sliding window algorithm
- Automatic cleanup of old entries

### Input Validation
- Docker image validation
- Command argument sanitization
- Environment variable validation
- Enhanced path traversal prevention

## Web UI Security

The web UI and API are **disabled by default**. If you enable them (`enable-web = true`), see [Web Package Security](./packages/web.md#security-considerations) for:

- Token authentication configuration
- Password hashing with bcrypt
- Rate limiting and CSRF protection
- Security headers

## Vulnerability Reporting

### Security Contact
- **Email**: security@netresearch.de
- **PGP Key**: Available on keyserver
- **Response Time**: 48 hours for acknowledgment

### Reporting Process
1. Send detailed vulnerability report to security contact
2. Include: description, impact, reproduction steps, suggested fix
3. Wait for acknowledgment (48 hours)
4. Allow 90 days for patch development
5. Coordinate disclosure timeline

### Bug Bounty
Currently not available. Please report vulnerabilities responsibly.

## Compliance

### Standards Alignment
- **OWASP Top 10** (2021): Full coverage
- **CWE Top 25**: Mitigations implemented
- **Docker CIS Benchmarks**: Level 1 compliance
- **NIST Cybersecurity Framework**: Core functions addressed

### Audit Logs
All security-relevant events are logged:
- Authentication attempts (success/failure)
- Authorization decisions
- Configuration changes
- Job executions
- Container operations
- API requests

**Log Retention**: 90 days minimum recommended

## Security Checklist

### Pre-Deployment

- [ ] Web auth enabled with bcrypt password hash
- [ ] Secret key configured (or auto-generated)
- [ ] HTTPS/TLS enabled (via reverse proxy)
- [ ] All secrets in environment variables
- [ ] Container resource limits set
- [ ] Non-root user configured
- [ ] Unnecessary capabilities dropped
- [ ] Docker socket access restricted
- [ ] LocalJobs from labels disabled
- [ ] Rate limiting enabled
- [ ] Firewall rules configured

### Post-Deployment

- [ ] Security scanning (Trivy/Clair)
- [ ] Vulnerability assessment
- [ ] Log monitoring enabled
- [ ] Alerting configured
- [ ] Backup tested
- [ ] Incident response plan documented
- [ ] Security training completed
- [ ] Access audit performed

### Ongoing

- [ ] Monthly vulnerability scans
- [ ] Quarterly access reviews
- [ ] Regular credential rotation (90 days)
- [ ] Security patch updates (within 7 days for high severity)
- [ ] Log review (weekly minimum)
- [ ] Incident response drills (quarterly)

## Related Documentation

- [ADR-002: Security Boundaries](./adr/ADR-002-security-boundaries.md) - Architectural decision on security responsibilities
- [Web Package](./packages/web.md) - Authentication and API security
- [Config Package](./packages/config.md) - Input validation and sanitization
- [Middlewares Package](./packages/middlewares.md) - Middleware security
- [Configuration Guide](./CONFIGURATION.md) - Secure configuration practices
- [PROJECT_INDEX](./PROJECT_INDEX.md) - Overall system architecture

---
*For security questions or vulnerability reports, contact: security@netresearch.de*
