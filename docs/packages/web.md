# Web Package

**Package**: `web`
**Path**: `/web/`
**Purpose**: HTTP server, API endpoints, JWT authentication, and health monitoring

## Overview

The web package provides a comprehensive HTTP interface for Ofelia, including RESTful API endpoints, JWT-based authentication, health checks, and security middleware. It exposes job management functionality through a web UI and API, with built-in rate limiting, CSRF protection, and secure authentication.

## Key Components

### 1. Server

HTTP server with RESTful API for job management and monitoring.

```go
type Server struct {
    addr      string
    scheduler *core.Scheduler
    config    interface{}
    srv       *http.Server
    origins   map[string]string
    client    *dockerclient.Client
}
```

**Features**:
- RESTful API for job management
- Web UI with embedded static files
- Security middleware integration
- Rate limiting per IP (100 req/min)
- Graceful shutdown support
- Health check endpoints

**Creation**:
```go
server := web.NewServer(":8080", scheduler, config, dockerClient)
```

### 2. Authentication System

#### JWT Authentication

Industry-standard JWT-based authentication with secure token management.

```go
type JWTManager struct {
    secretKey   []byte
    tokenExpiry time.Duration
}

type Claims struct {
    Username string `json:"username"`
    jwt.RegisteredClaims
}
```

**Features**:
- HS256 signing algorithm
- Configurable expiry (default: hours)
- Token refresh capability
- HTTP middleware integration
- Cookie and header token support

**Usage**:
```go
// Create JWT manager
jwtManager, err := web.NewJWTManager(secretKey, 24) // 24 hour expiry
if err != nil {
    log.Fatal(err)
}

// Generate token
token, err := jwtManager.GenerateToken("admin")

// Validate token
claims, err := jwtManager.ValidateToken(token)

// Refresh token
newToken, err := jwtManager.RefreshToken(oldToken)

// Apply middleware
protectedHandler := jwtManager.Middleware(apiHandler)
```

**Security Requirements**:
- Secret key must be ≥32 characters
- Tokens include standard JWT claims (exp, iat, nbf, iss, sub)
- Automatic signing method validation

#### Secure Authentication

Enhanced authentication with bcrypt, rate limiting, and CSRF protection.

```go
type SecureAuthConfig struct {
    Enabled      bool
    Username     string
    PasswordHash string // bcrypt hash
    SecretKey    string
    TokenExpiry  int    // hours
    MaxAttempts  int    // per minute
}
```

**Features**:
- Bcrypt password hashing (cost 12)
- Constant-time username comparison
- Rate limiting per IP
- CSRF token protection
- Timing attack prevention
- Secure HTTP-only cookies

**Password Hashing**:
```go
// Generate bcrypt hash (cost 12)
hash, err := web.HashPassword("mySecurePassword")

// Store hash in config
config.PasswordHash = hash
```

**Rate Limiting**:
```go
// Create rate limiter: 5 attempts per minute
rateLimiter := web.NewRateLimiter(5, 5)

// Check if allowed
if !rateLimiter.Allow(clientIP) {
    return errors.New("too many attempts")
}
```

**CSRF Protection**:
```go
// Generate CSRF token
csrfToken, err := tokenManager.GenerateCSRFToken()

// Validate CSRF token (one-time use)
valid := tokenManager.ValidateCSRFToken(token)
```

### 3. Health Checks

Comprehensive health monitoring with Docker, scheduler, and system checks.

```go
type HealthChecker struct {
    startTime     time.Time
    dockerClient  *docker.Client
    version       string
    checks        map[string]HealthCheck
    checkInterval time.Duration // default: 30s
}
```

**Health Status Levels**:
```go
const (
    HealthStatusHealthy   = "healthy"
    HealthStatusDegraded  = "degraded"
    HealthStatusUnhealthy = "unhealthy"
)
```

**Checks Performed**:
1. **Docker Connectivity**: Ping Docker daemon, get container count
2. **Scheduler Status**: Verify scheduler is operational
3. **System Resources**: Monitor memory usage (healthy <75%, degraded <90%, unhealthy ≥90%)

**Usage**:
```go
// Create health checker
healthChecker := web.NewHealthChecker(dockerClient, "1.0.0")

// Register health endpoints
server.RegisterHealthEndpoints(healthChecker)

// Health endpoints available:
// GET /health     - Detailed health information (always 200 OK)
// GET /healthz    - Simple health check alias
// GET /ready      - Readiness check (503 if unhealthy)
// GET /live       - Liveness check (always 200 OK)
```

**Health Response**:
```json
{
  "status": "healthy",
  "timestamp": "2025-01-15T10:30:00Z",
  "uptimeSeconds": 3600.5,
  "version": "1.0.0",
  "checks": {
    "docker": {
      "name": "docker",
      "status": "healthy",
      "message": "Docker 24.0.7 running with 5 containers",
      "lastChecked": "2025-01-15T10:30:00Z",
      "durationMs": 12
    },
    "scheduler": {
      "name": "scheduler",
      "status": "healthy",
      "message": "Scheduler is operational",
      "lastChecked": "2025-01-15T10:30:00Z",
      "durationMs": 1
    },
    "system": {
      "name": "system",
      "status": "healthy",
      "message": "System resources normal",
      "lastChecked": "2025-01-15T10:30:00Z",
      "durationMs": 2
    }
  },
  "system": {
    "goVersion": "go1.23.5",
    "goroutines": 12,
    "cpus": 8,
    "memoryAllocBytes": 45678900,
    "memoryTotalBytes": 67890123,
    "gcRuns": 45
  }
}
```

### 4. Security Middleware

HTTP middleware for security headers and rate limiting.

**Security Headers**:
```go
func securityHeaders(next http.Handler) http.Handler
```

**Headers Applied**:
- `X-Content-Type-Options: nosniff` - Prevent MIME sniffing
- `X-Frame-Options: DENY` - Prevent clickjacking
- `X-XSS-Protection: 1; mode=block` - XSS protection
- `Referrer-Policy: strict-origin-when-cross-origin` - Referrer control
- `Content-Security-Policy` - CSP for XSS prevention
- `Strict-Transport-Security` - HSTS (when using HTTPS)

**Rate Limiting**:
```go
type rateLimiter struct {
    requests map[string][]time.Time
    limit    int           // requests allowed
    window   time.Duration // time window
}

// Default: 100 requests per minute per IP
rl := newRateLimiter(100, time.Minute)
```

**Features**:
- Per-IP rate limiting
- Sliding window algorithm
- Automatic cleanup of old entries
- X-Forwarded-For support

### 5. API Endpoints

#### Job Management

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/jobs` | GET | List all active jobs |
| `/api/jobs/removed` | GET | List removed jobs |
| `/api/jobs/disabled` | GET | List disabled jobs |
| `/api/jobs/run` | POST | Trigger job execution |
| `/api/jobs/disable` | POST | Disable a job |
| `/api/jobs/enable` | POST | Enable a job |
| `/api/jobs/create` | POST | Create new job |
| `/api/jobs/update` | POST | Update job configuration |
| `/api/jobs/delete` | POST | Delete a job |
| `/api/jobs/{name}/history` | GET | Get job execution history |
| `/api/config` | GET | Get server configuration (jobs stripped) |

#### Job API Types

**Job Response**:
```go
type apiJob struct {
    Name     string          `json:"name"`
    Type     string          `json:"type"`     // "run", "exec", "local", "service", "compose"
    Schedule string          `json:"schedule"`
    Command  string          `json:"command"`
    LastRun  *apiExecution   `json:"lastRun,omitempty"`
    Origin   string          `json:"origin"`   // "config", "docker", "api"
    Config   json.RawMessage `json:"config"`
}
```

**Execution Response**:
```go
type apiExecution struct {
    Date     time.Time     `json:"date"`
    Duration time.Duration `json:"duration"`
    Failed   bool          `json:"failed"`
    Skipped  bool          `json:"skipped"`
    Error    string        `json:"error,omitempty"`
    Stdout   string        `json:"stdout"`
    Stderr   string        `json:"stderr"`
}
```

## API Usage Examples

### List All Jobs

```bash
GET /api/jobs
```

**Response**:
```json
[
  {
    "name": "backup-db",
    "type": "exec",
    "schedule": "@daily",
    "command": "pg_dump mydb",
    "lastRun": {
      "date": "2025-01-15T02:00:00Z",
      "duration": 45200000000,
      "failed": false,
      "skipped": false,
      "stdout": "Backup completed successfully",
      "stderr": ""
    },
    "origin": "config"
  }
]
```

### Run Job Manually

```bash
POST /api/jobs/run
Content-Type: application/json

{
  "name": "backup-db"
}
```

**Response**: `204 No Content` on success

### Create New Job

```bash
POST /api/jobs/create
Content-Type: application/json

{
  "name": "new-job",
  "type": "local",
  "schedule": "0 */6 * * *",
  "command": "/backup/script.sh"
}
```

**Response**: `201 Created` on success

### Get Job History

```bash
GET /api/jobs/backup-db/history
```

**Response**:
```json
[
  {
    "date": "2025-01-15T02:00:00Z",
    "duration": 45200000000,
    "failed": false,
    "skipped": false,
    "stdout": "Backup completed",
    "stderr": ""
  },
  {
    "date": "2025-01-14T02:00:00Z",
    "duration": 43100000000,
    "failed": false,
    "skipped": false,
    "stdout": "Backup completed",
    "stderr": ""
  }
]
```

## Authentication Flow

### JWT Authentication

```bash
# 1. Generate token
POST /api/login
Content-Type: application/json

{
  "username": "admin",
  "password": "secure123"
}

# Response:
{
  "token": "eyJhbGc...",
  "expires_in": 86400
}

# 2. Use token in requests
GET /api/jobs
Authorization: Bearer eyJhbGc...

# 3. Refresh token before expiry
POST /api/refresh
Authorization: Bearer eyJhbGc...

# Response:
{
  "token": "eyJhbGc...", // New token
  "expires_in": 86400
}
```

### Secure Authentication with CSRF

```bash
# 1. Login with CSRF protection
POST /api/login
Content-Type: application/json
X-CSRF-Token: abc123...

{
  "username": "admin",
  "password": "secure123"
}

# Response:
{
  "token": "auth_token_here",
  "csrf_token": "new_csrf_token",
  "expires_in": 86400
}

# Cookie set: auth_token (HttpOnly, Secure, SameSite=Strict)
```

## Server Configuration

### Basic Setup

```go
import (
    "github.com/netresearch/ofelia/web"
    "github.com/netresearch/ofelia/core"
)

func main() {
    // Create scheduler
    scheduler := core.NewScheduler()

    // Create server
    server := web.NewServer(":8080", scheduler, config, dockerClient)

    // Create health checker
    healthChecker := web.NewHealthChecker(dockerClient, "1.0.0")
    server.RegisterHealthEndpoints(healthChecker)

    // Start server
    if err := server.Start(); err != nil {
        log.Fatal(err)
    }

    // Graceful shutdown
    <-ctx.Done()
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    if err := server.Shutdown(shutdownCtx); err != nil {
        log.Printf("Server shutdown error: %v", err)
    }
}
```

### With JWT Authentication

```go
// Create JWT manager
jwtManager, err := web.NewJWTManager(os.Getenv("OFELIA_JWT_SECRET"), 24)
if err != nil {
    log.Fatal(err)
}

// Create server with JWT middleware
server := web.NewServer(":8080", scheduler, config, dockerClient)

// Apply JWT authentication to API routes
// (Implementation depends on routing strategy)
```

### With Secure Authentication

```go
// Create secure auth config
authConfig := &web.SecureAuthConfig{
    Enabled:      true,
    Username:     "admin",
    PasswordHash: hashedPassword, // bcrypt hash
    SecretKey:    os.Getenv("OFELIA_SECRET_KEY"),
    TokenExpiry:  24,
    MaxAttempts:  5,
}

// Create token manager and rate limiter
tokenManager, _ := web.NewSecureTokenManager(authConfig.SecretKey, authConfig.TokenExpiry)
rateLimiter := web.NewRateLimiter(authConfig.MaxAttempts, authConfig.MaxAttempts)

// Create login handler
loginHandler := web.NewSecureLoginHandler(authConfig, tokenManager, rateLimiter)
```

## Security Considerations

### Password Security

- **Bcrypt hashing**: Cost factor 12 for security/performance balance
- **Constant-time comparison**: Prevents timing attacks on username
- **Rate limiting**: Prevent brute force (default: 5 attempts/minute)
- **Delay on failure**: 100ms delay to slow brute force

### Token Security

- **JWT secret key**: Must be ≥32 characters for HS256
- **Token expiry**: Configurable (default: 24 hours)
- **CSRF protection**: One-time use tokens for state-changing operations
- **Secure cookies**: HttpOnly, Secure (HTTPS), SameSite=Strict

### Network Security

- **HTTPS enforcement**: HSTS header when TLS detected
- **Security headers**: XSS protection, frame denial, CSP
- **Rate limiting**: 100 requests/minute per IP (configurable)
- **Request timeouts**: ReadHeader 5s, Write 60s, Idle 120s

### Input Validation

- **Method validation**: Enforce POST for state changes
- **Content-type checks**: Validate JSON payloads
- **Origin tracking**: Track job creation source (config, docker, API)

## Performance Considerations

### Server Timeouts

```go
server.srv = &http.Server{
    Addr:              ":8080",
    ReadHeaderTimeout: 5 * time.Second,  // Prevent slow header attacks
    WriteTimeout:      60 * time.Second, // Long-running jobs need time
    IdleTimeout:       120 * time.Second, // Keep-alive timeout
}
```

### Rate Limiting

- **Default**: 100 requests/minute per IP
- **Cleanup**: Periodic cleanup every window duration
- **Memory**: O(n) where n = unique IPs in window

### Health Checks

- **Interval**: 30 seconds (configurable)
- **Overhead**: <15ms per check cycle
- **Docker ping**: ~10ms
- **System stats**: ~2ms

## Integration Points

### Core Integration

- **[Scheduler](../../core/scheduler.go)**: Job management operations
- **[Jobs](../../core/job.go)**: Job execution and history
- **[Docker Client](../../core/docker_client.go)**: Container operations

### Metrics Integration

- **[Prometheus Metrics](./metrics.md)**: HTTP request metrics
- **Endpoint**: `/metrics` for Prometheus scraping

### Logging Integration

- **[Structured Logging](./logging.md)**: Request/response logging
- **Correlation IDs**: Track requests across components

## Testing

### Health Check Testing

```bash
# Liveness probe (always succeeds if running)
curl http://localhost:8080/live
# Response: OK

# Readiness probe (checks dependencies)
curl http://localhost:8080/ready
# Response: {"status":"healthy",...}

# Detailed health check
curl http://localhost:8080/health
# Response: Full health report with all checks
```

### API Testing

```bash
# Test rate limiting
for i in {1..150}; do
  curl http://localhost:8080/api/jobs &
done
# Expected: 100 succeed, 50 fail with 429 Too Many Requests
```

### Security Testing

```bash
# Test security headers
curl -I http://localhost:8080/
# Expected: X-Content-Type-Options, X-Frame-Options, etc.

# Test JWT authentication
curl http://localhost:8080/api/jobs
# Expected: 401 Unauthorized without token

curl -H "Authorization: Bearer <token>" http://localhost:8080/api/jobs
# Expected: 200 OK with valid token
```

## Troubleshooting

### Authentication Issues

```
Error: "JWT secret key must be at least 32 characters long"
Solution: Set OFELIA_JWT_SECRET environment variable with ≥32 chars
```

```
Error: "Invalid or expired token"
Solution: Generate new token via /api/login or refresh existing token
```

```
Error: "Too many login attempts"
Solution: Wait for rate limit window to reset (default: 1 minute)
```

### Health Check Issues

```
Error: Docker check shows "unhealthy"
Solution: Verify Docker daemon is running and accessible
```

```
Error: System check shows "degraded" - memory usage high
Solution: Check memory usage, restart if >90% allocation
```

### Server Issues

```
Error: "Address already in use"
Solution: Change port or stop conflicting process
```

```
Error: Rate limit exceeded
Solution: Reduce request rate or increase limit in server configuration
```

## Related Documentation

- [Core Package](./core.md) - Job execution and scheduling
- [Metrics Package](./metrics.md) - HTTP metrics collection
- [Logging Package](./logging.md) - Request logging
- [API Documentation](../API.md) - Complete API reference
- [Security Considerations](../SECURITY.md) - Security best practices
- [PROJECT_INDEX](../PROJECT_INDEX.md) - Overall system architecture
