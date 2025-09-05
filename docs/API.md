# Ofelia API Documentation

## Overview
Ofelia provides a RESTful API for job management, monitoring, and configuration. All API endpoints are available when the web UI is enabled with `--enable-web`.

## Authentication

### JWT Authentication
The API uses JWT tokens for authentication when security is enabled.

#### Login
```http
POST /api/login
Content-Type: application/json

{
  "username": "admin",
  "password": "secret"
}
```

**Response:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires": "2024-01-01T00:00:00Z"
}
```

#### Using the Token
Include the JWT token in the Authorization header:
```http
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

#### Refresh Token
```http
POST /api/refresh
Authorization: Bearer <current-token>
```

**Response:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires": "2024-01-01T00:00:00Z"
}
```

## Job Management

### List All Jobs
```http
GET /api/jobs
```

**Response:**
```json
{
  "jobs": [
    {
      "name": "backup",
      "type": "exec",
      "schedule": "@daily",
      "command": "backup.sh",
      "container": "myapp",
      "lastRun": "2024-01-01T00:00:00Z",
      "nextRun": "2024-01-02T00:00:00Z",
      "status": "success",
      "origin": "config",
      "enabled": true
    },
    {
      "name": "cleanup",
      "type": "run",
      "schedule": "0 2 * * *",
      "image": "alpine:latest",
      "command": "cleanup.sh",
      "lastRun": "2024-01-01T02:00:00Z",
      "nextRun": "2024-01-02T02:00:00Z",
      "status": "running",
      "origin": "docker-label",
      "enabled": true
    }
  ],
  "total": 2,
  "running": 1
}
```

### Get Job Details
```http
GET /api/job/{name}
```

**Response:**
```json
{
  "name": "backup",
  "type": "exec",
  "schedule": "@daily",
  "command": "backup.sh",
  "container": "myapp",
  "user": "root",
  "environment": ["BACKUP_DIR=/data"],
  "tty": false,
  "lastRun": "2024-01-01T00:00:00Z",
  "nextRun": "2024-01-02T00:00:00Z",
  "status": "success",
  "origin": "config",
  "enabled": true,
  "history": [
    {
      "executionId": "550e8400-e29b-41d4-a716-446655440000",
      "startTime": "2024-01-01T00:00:00Z",
      "endTime": "2024-01-01T00:05:00Z",
      "duration": 300000000000,
      "status": "success",
      "output": "Backup completed successfully",
      "error": null
    }
  ],
  "metrics": {
    "totalRuns": 30,
    "successfulRuns": 29,
    "failedRuns": 1,
    "averageDuration": 285000000000,
    "lastSuccess": "2024-01-01T00:00:00Z",
    "lastFailure": "2023-12-15T00:00:00Z"
  }
}
```

### Trigger Job Execution
```http
POST /api/job/{name}/run
```

**Response:**
```json
{
  "executionId": "550e8400-e29b-41d4-a716-446655440001",
  "job": "backup",
  "startTime": "2024-01-01T12:00:00Z",
  "status": "running"
}
```

### Update Job Configuration
```http
PUT /api/job/{name}
Content-Type: application/json

{
  "schedule": "0 3 * * *",
  "command": "backup.sh --verbose",
  "environment": ["BACKUP_DIR=/data", "COMPRESSION=gzip"]
}
```

**Response:**
```json
{
  "name": "backup",
  "updated": true,
  "message": "Job configuration updated successfully"
}
```

### Delete Job
```http
DELETE /api/job/{name}
```

**Response:**
```json
{
  "name": "backup",
  "deleted": true,
  "message": "Job deleted successfully"
}
```

### Enable/Disable Job
```http
PATCH /api/job/{name}/toggle
```

**Response:**
```json
{
  "name": "backup",
  "enabled": false,
  "message": "Job disabled"
}
```

## Execution Management

### Get Execution Status
```http
GET /api/execution/{executionId}
```

**Response:**
```json
{
  "executionId": "550e8400-e29b-41d4-a716-446655440001",
  "job": "backup",
  "startTime": "2024-01-01T12:00:00Z",
  "endTime": "2024-01-01T12:05:00Z",
  "duration": 300000000000,
  "status": "success",
  "output": "Backup completed\n1000 files processed",
  "error": null,
  "exitCode": 0
}
```

### Stop Running Execution
```http
POST /api/execution/{executionId}/stop
```

**Response:**
```json
{
  "executionId": "550e8400-e29b-41d4-a716-446655440001",
  "stopped": true,
  "message": "Execution stopped"
}
```

### Get Execution Logs
```http
GET /api/execution/{executionId}/logs
```

**Response:**
```json
{
  "executionId": "550e8400-e29b-41d4-a716-446655440001",
  "logs": [
    {
      "timestamp": "2024-01-01T12:00:00Z",
      "level": "info",
      "message": "Starting backup process"
    },
    {
      "timestamp": "2024-01-01T12:00:01Z",
      "level": "info",
      "message": "Scanning files..."
    },
    {
      "timestamp": "2024-01-01T12:05:00Z",
      "level": "info",
      "message": "Backup completed successfully"
    }
  ]
}
```

## Scheduler Control

### Get Scheduler Status
```http
GET /api/scheduler/status
```

**Response:**
```json
{
  "running": true,
  "startTime": "2024-01-01T00:00:00Z",
  "uptime": 86400,
  "totalJobs": 15,
  "activeJobs": 12,
  "runningJobs": 2,
  "nextJob": {
    "name": "cleanup",
    "scheduledTime": "2024-01-02T02:00:00Z"
  }
}
```

### Start/Stop Scheduler
```http
POST /api/scheduler/start
POST /api/scheduler/stop
```

**Response:**
```json
{
  "running": true,
  "message": "Scheduler started"
}
```

### Reload Configuration
```http
POST /api/scheduler/reload
```

**Response:**
```json
{
  "reloaded": true,
  "jobsAdded": 2,
  "jobsUpdated": 1,
  "jobsRemoved": 0,
  "message": "Configuration reloaded successfully"
}
```

## Monitoring

### Health Check
```http
GET /health/liveness
```

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2024-01-01T12:00:00Z"
}
```

### Readiness Check
```http
GET /health/readiness
```

**Response:**
```json
{
  "ready": true,
  "scheduler": "running",
  "docker": "connected",
  "database": "n/a"
}
```

### Prometheus Metrics
```http
GET /metrics
```

**Response:**
```text
# HELP ofelia_jobs_total Total number of jobs executed
# TYPE ofelia_jobs_total counter
ofelia_jobs_total 1234

# HELP ofelia_jobs_failed_total Total number of failed jobs
# TYPE ofelia_jobs_failed_total counter
ofelia_jobs_failed_total 12

# HELP ofelia_jobs_running Number of currently running jobs
# TYPE ofelia_jobs_running gauge
ofelia_jobs_running 2

# HELP ofelia_job_duration_seconds Job execution duration in seconds
# TYPE ofelia_job_duration_seconds histogram
ofelia_job_duration_seconds_bucket{le="0.1"} 100
ofelia_job_duration_seconds_bucket{le="0.5"} 500
...
```

## Configuration

### Get Current Configuration
```http
GET /api/config
```

**Response:**
```json
{
  "global": {
    "dockerHost": "unix:///var/run/docker.sock",
    "dockerPollInterval": 30,
    "dockerEvents": true,
    "slackURL": "https://hooks.slack.com/...",
    "emailFrom": "ofelia@example.com",
    "emailTo": "admin@example.com"
  },
  "jobs": {
    "exec": [...],
    "run": [...],
    "local": [...],
    "service": [...]
  }
}
```

### Update Global Configuration
```http
PATCH /api/config/global
Content-Type: application/json

{
  "dockerPollInterval": 60,
  "slackChannel": "#alerts"
}
```

## WebSocket Events

### Subscribe to Real-time Events
```javascript
const ws = new WebSocket('ws://localhost:8080/api/ws');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Event:', data.type, data.payload);
};
```

**Event Types:**
- `job.started`: Job execution started
- `job.completed`: Job execution completed
- `job.failed`: Job execution failed
- `job.added`: New job added
- `job.updated`: Job configuration updated
- `job.removed`: Job removed
- `scheduler.started`: Scheduler started
- `scheduler.stopped`: Scheduler stopped

## Error Responses

All endpoints return consistent error responses:

```json
{
  "error": {
    "code": "JOB_NOT_FOUND",
    "message": "Job 'backup' not found",
    "details": {
      "job": "backup",
      "timestamp": "2024-01-01T12:00:00Z"
    }
  }
}
```

### Error Codes
- `UNAUTHORIZED`: Missing or invalid authentication
- `FORBIDDEN`: Insufficient permissions
- `JOB_NOT_FOUND`: Job does not exist
- `INVALID_SCHEDULE`: Invalid cron expression
- `DOCKER_ERROR`: Docker API error
- `EXECUTION_ERROR`: Job execution failed
- `VALIDATION_ERROR`: Invalid input data
- `INTERNAL_ERROR`: Server error

## Rate Limiting

API requests are rate-limited to prevent abuse:

- **Default limit**: 100 requests per minute
- **Burst capacity**: 10 requests

Rate limit headers:
```http
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1704067200
```

## CORS Configuration

Cross-Origin Resource Sharing (CORS) can be configured:

```yaml
cors:
  allowed_origins:
    - http://localhost:3000
    - https://app.example.com
  allowed_methods:
    - GET
    - POST
    - PUT
    - DELETE
  allowed_headers:
    - Authorization
    - Content-Type
  max_age: 86400
```

---
*See also: [Web Package](./packages/web.md) | [Configuration Guide](./CONFIGURATION.md) | [Project Index](./PROJECT_INDEX.md)*