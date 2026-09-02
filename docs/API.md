# Ofelia API Documentation

## Overview

Ofelia serves a JSON API when the web server is enabled (`--enable-web`, default address `:8081`).
Every API route lives under the `/api/` prefix; the health endpoints live at the server root.
This document describes the endpoints that exist in the code (`web/server.go` — the `routes`
table is the source of truth); if you find a mismatch, that is a bug in this file.

Two general rules:

- All mutating endpoints are `POST` and take a JSON body. Any other method returns
  `405 Method Not Allowed`.
- A malformed JSON body returns `400 Bad Request`; an unknown job name returns
  `404 Not Found`; internal failures return `500 Internal Server Error` with a short
  plain-text message.

## Authentication

Authentication is optional and disabled unless the daemon is configured with web auth.
When disabled, the auth endpoints below are not registered and every other endpoint is open.

When enabled, all `/api/` routes except `/api/login`, `/api/auth/status` and
`/api/csrf-token` require authentication via either:

- the `auth_token` cookie set by login (`HttpOnly`, `SameSite=Strict`; `Secure` when the
  request arrived over TLS or with `X-Forwarded-Proto: https`), or
- an `Authorization: Bearer <token>` header carrying the token returned by login.

### Login

```http
POST /api/login
Content-Type: application/json

{"username": "admin", "password": "secret"}
```

Response `200 OK` (and the `auth_token` cookie):

```json
{"token": "<token>", "csrf_token": "<csrf-token>", "expires_in": 86400}
```

Failed credentials return `401 Unauthorized`; repeated failures are rate limited per client IP.
There is no refresh endpoint — log in again when the token expires.

### Logout

```http
POST /api/logout
```

Clears the auth cookie. Returns `204 No Content`.

### Auth status

```http
GET /api/auth/status
```

Reports whether authentication is enabled and whether the caller is authenticated.

### CSRF token

```http
GET /api/csrf-token
```

Returns the CSRF token for the session; the web UI sends it with the login form.

## Jobs

### List jobs

```http
GET /api/jobs            # active jobs
GET /api/jobs/removed    # jobs removed from the configuration
GET /api/jobs/disabled   # disabled (paused) jobs
```

Each returns a JSON **array** of job objects:

```json
[
  {
    "name": "backup",
    "type": "run",
    "schedule": "@daily",
    "command": "backup.sh",
    "running": false,
    "lastRun": {
      "date": "2026-08-26T00:00:00Z",
      "duration": 300000000000,
      "failed": false,
      "skipped": false,
      "stdout": "…",
      "stderr": ""
    },
    "nextRuns": ["2026-08-27T00:00:00Z"],
    "prevRuns": ["2026-08-26T00:00:00Z"],
    "origin": "ini",
    "config": {"…": "full job configuration as JSON"},
    "recentRuns": [
      {
        "date": "2026-08-26T00:00:00Z",
        "duration": 300000000000,
        "failed": false,
        "skipped": false
      }
    ]
  }
]
```

`lastRun` is omitted when the job has never run. `duration` values are Go
`time.Duration` nanoseconds. `origin` is one of `ini`, `label`, `api`, `web`.

`recentRuns` summarises up to the ten newest executions, oldest first, so a list
view can show outcome history without a history request per job. It is omitted
for jobs that have not run, and on `/api/jobs/removed`, where nothing reads it.
Use `/api/jobs/{name}/history` for the full records including output.

### Job history

```http
GET /api/jobs/{name}/history
```

Returns a JSON array of executions in the shape of `lastRun` above (plus an
`error` string when the execution errored). `404 Not Found` for unknown jobs or
job types without history.

### Run a job now

```http
POST /api/jobs/run
Content-Type: application/json

{"name": "backup"}
```

Returns `204 No Content` on success.

### Disable / enable a job

```http
POST /api/jobs/disable
POST /api/jobs/enable
Content-Type: application/json

{"name": "backup"}
```

Returns `204 No Content`. Works for jobs of every origin — disabling an
INI/label job from the API is the supported way to suppress it temporarily, and
the disabled state is persisted across restarts.

### Create a job

```http
POST /api/jobs/create
Content-Type: application/json

{
  "name": "cleanup",
  "type": "run",
  "schedule": "0 2 * * *",
  "image": "alpine:latest",
  "command": "cleanup.sh"
}
```

The request body (`jobRequest`) accepts:

| Field | Type | Notes |
|---|---|---|
| `name` | string | required; ≤256 chars, no control characters |
| `type` | string | `run`, `exec`, `local`, `compose`; omitted means `local`. Anything else is rejected with `unknown job type`. Service jobs cannot be created over the API — see [#816](https://github.com/netresearch/ofelia/issues/816) |
| `schedule` | string | cron expression or `@shortcut` |
| `command` | string | |
| `image` | string | `run` jobs |
| `container` | string | `exec` jobs |
| `file` | string | `compose` jobs |
| `service` | string | `compose` jobs: the service to run or exec |
| `exec` | bool | `compose` jobs: exec in a running service |
| `maxRuntime` | string | `run` jobs; Go duration, e.g. `30m`. Omitting it inherits `[global] max-runtime`, and falls back to the scheduler's 24h default only when no global is set; `"0s"` means the same and is not a way to ask for no bound. Same ladder an INI `[job-run]` section climbs ([#806](https://github.com/netresearch/ofelia/issues/806)) |

Returns `201 Created`. Validation failures return `400 Bad Request`.
API-created jobs are persisted and survive restarts.

A name owned by the INI file or by Docker labels returns `403 Forbidden`, the
same gate update and delete apply. The gate is on the *name*, not on a
registered job: a config job with an empty or malformed schedule holds no cron
entry, so nothing else would stop a create from taking its name and replacing
it with a caller-chosen job.

### Update a job

```http
POST /api/jobs/update
Content-Type: application/json
```

Same body as create. Full replace: omitted optional fields reset to their
defaults. Returns `200 OK` when an existing job was updated, `201 Created` when
the job did not exist and was created.

Jobs owned by the INI file or Docker labels return `403 Forbidden`, mirroring
the delete gate — change them at their source, or use `/api/jobs/disable` to
suppress them.

### Delete a job

```http
POST /api/jobs/delete
Content-Type: application/json

{"name": "cleanup"}
```

Returns `204 No Content`. Jobs owned by the INI file or Docker labels return
`403 Forbidden` — edit the owning configuration to remove them (or use
`/api/jobs/disable` to suppress them); only API/web-created jobs can be deleted
here.

## Dashboard

```http
GET /api/dashboard
GET /api/dashboard?history=<job name>
GET /api/dashboard?history=<job name>&historyFp=<fingerprint>
```

One aggregate snapshot for polling clients: the three job lists, the stripped
configuration, and — with `?history=` — that job's executions, all from a single
moment in time.

```json
{
  "jobs": [],
  "disabled": [],
  "removed": [],
  "config": {"…": "same payload as GET /api/config"},
  "history": null
}
```

`jobs`, `disabled` and `removed` hold the job objects described under
[List jobs](#list-jobs). `history` is `null` when the query parameter is absent
or names no job, and `[]` for an existing job with no executions — a vanished
job does not fail the request.

A response carrying history also carries `historyFingerprint`. Pass it back as
`&historyFp=<value>` and the next response omits `history` while it still
matches, instead of re-sending every run's full output on each tick. Clients
that do not send the parameter always receive the history.

The endpoint is additive and exists for clients that would otherwise issue four
or five requests per tick (the bundled web UI polls it every five seconds); the
per-resource endpoints above are unchanged. It is authenticated like every other
`/api/` route.

## Configuration

```http
GET /api/config
```

Returns the running daemon configuration as JSON, with the job collections
stripped (use the job endpoints for those). The same stripped payload is the
`config` section of `GET /api/dashboard`.

## Health

Registered at the server root (not under `/api/`, no authentication):

```http
GET /health    # aggregate health report
GET /healthz   # alias of /health
GET /ready     # readiness probe
GET /live      # liveness probe
```

## Errors

Errors are plain-text messages with the appropriate status code:
`400` invalid body or validation failure, `401` unauthenticated (auth enabled),
`403` config-owned job on update or delete, `404` unknown job, `405` wrong
method, `500` internal failure. Login attempts are rate limited per client IP.

Every response is compressed when the client advertises a codec the server
supports: zstd is preferred, gzip is the fallback, and a client advertising
neither receives the identity encoding. Responses carry
`Vary: Accept-Encoding`.
