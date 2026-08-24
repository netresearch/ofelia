<!-- Managed by agent: keep sections and order; edit content, not structure. Last updated: 2025-09-29 -->

# AGENTS.md — Web (HTTP Server & Authentication)

## Overview
- HTTP server, REST API, and web interface for Ofelia
- Main entry points: `server.go`, `auth.go`, `health.go`
- JWT-based authentication with secure session management
- Health checks and monitoring endpoints

## Setup & environment
- Install: `go mod download`
- Run server: `./ofelia daemon` (includes web server)
- Test web: `go test ./web/...`
- Default port: 8080 (configurable)

## Build & tests (prefer file-scoped)
- Typecheck package: `go build ./web`
- Lint file: `golangci-lint run ./web/server.go`
- Format file: `gofmt -w ./web/server.go`
- Run tests for this package: `go test ./web/...`
- Test with race detection: `go test -race ./web/...`

## Code style & conventions
- Use `*slog.Logger` from stdlib `log/slog` for all logging
- HTTP handlers follow standard `http.HandlerFunc` pattern
- Middleware chaining for authentication and logging
- JSON responses use proper HTTP status codes
- Context propagation through request lifecycle
- Graceful shutdown handling with proper cleanup

## Web UI (`static/ui/`: styles.css, app.js, templates/)
- Server-rendered with embedded Go templates, progressive enhancement via embedded JavaScript
- Served via `//go:embed ui/*` in `static/static.go` — new files auto-included
- Uses **Pico CSS v2** (classless/semantic, auto dark mode) from external styles.css
- Pico dark mode: `data-theme` must be **absent** for auto (NOT set to `"auto"`)
- Semantic color vars: `--pico-ins-color` (green), `--pico-del-color` (red), `--pico-mark-background-color` (yellow)
- All user data must be escaped via `escapeHtml()` before innerHTML insertion
- Use `data-*` attributes + event delegation, not inline `onclick`
- Docker logs may contain ASCII control characters — clean them in the UI with `stripControlChars()`; stdout/stderr stream demuxing is handled server-side via `stdcopy.StdCopy` in the Docker adapter.

## API endpoints & polling (`server.go`, `dashboard.go`)
- Per-resource endpoints (`/api/jobs`, `/api/jobs/disabled`, `/api/jobs/removed`, `/api/config`, `/api/jobs/<name>/history`) are the documented public HTTP API (docs/API.md) — external scripts and monitoring consume them. Never remove or reshape them for UI needs.
- `/api/dashboard` is an additive aggregate for polling consumers: the UI's 5s tick fetches it once (optionally with `?history=<job>`) instead of issuing 4–5 separate requests. Rationale: the separate-requests poll cost ~60 req/min per browser tab and exhausted the 100/min per-IP rate limiter with two dashboard tabs open, which 429'd everything including static assets and broke the page. One request per tick also gives the UI a consistent snapshot.
- The rate limiter counts ALL requests, static assets included — keep the UI's request budget low; render-only interactions (search, sort) must never refetch. Only `/live` and `/ready` are exempt (a probe answered 429 gets the daemon restarted); `/health` and `/healthz` are token-free but counted, because `GetHealth` calls `runtime.ReadMemStats` (stop-the-world) per request — an exemption there would be an unauthenticated, unthrottled GC pause.
- New routes go through `Server.routes` so `TestRouteAuthExpectations` holds them to an auth expectation automatically; public (token-free) routes must additionally be declared in `TestPublicRoutesAreExactlyDeclared`.
- All responses are compressed for clients that advertise a supported codec (`compress.go`, innermost middleware, delegating to `klauspost/compress/gzhttp`; Range requests bypass it). The codec is **zstd or gzip**, not gzip alone: `gzhttp.NewWrapper` enables zstd and prefers it at equal q-values, so Chrome/Edge/Firefox (`gzip, deflate, br, zstd`) get zstd and Safari falls back to gzip — both pinned in `compress_test.go`. gzhttp's writer supports `http.Flusher`, so SSE-style streaming works through it; verify any future WebSocket/Hijacker endpoint explicitly. Endpoints that echo user input next to a secret in one response would reopen BREACH-style length leaks.

## CSP headers (`middleware.go`)
- `script-src 'self' 'unsafe-inline'` — only the pre-paint theme/density script in templates/layout.html is inline; app.js is external
- `style-src 'self' 'unsafe-inline'` — styles are external (styles.css) but `'unsafe-inline'` is retained for inline style attributes in the markup (e.g. `style="display:none"` in templates)
- `img-src 'self' data:` — required for Pico CSS inline SVG data URIs

## Security & safety
- JWT tokens: use secure signing, proper expiration, rotation
- Authentication: never log credentials, use secure headers
- CORS: configure appropriately for production
- Rate limiting: implement to prevent abuse
- Input validation: sanitize all user inputs; escape HTML for web UI
- HTTPS: enforce in production environments
- Session management: secure cookie settings

## PR/commit checklist
- [ ] All tests pass: `go test ./web/...`
- [ ] Authentication flows tested
- [ ] HTTP status codes are appropriate
- [ ] Security headers properly set
- [ ] Input validation comprehensive
- [ ] No credentials in logs or responses

## Good vs. bad examples
- Good: `auth.go` (secure JWT handling)
- Good: `server.go` (proper middleware chaining)
- Good: `health.go` (monitoring endpoint patterns)
- Bad: Hardcoded secrets in source code
- Bad: Missing input validation on endpoints

## When stuck
- Review JWT patterns in `jwt_auth.go` and `jwt_handlers.go`
- Check middleware patterns in `middleware.go`
- Look at health check implementation in `health.go`
- Reference authentication migration in `auth_migration.go`