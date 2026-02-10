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
- Use `core.Logger` for all logging, never direct `log` package
- HTTP handlers follow standard `http.HandlerFunc` pattern
- Middleware chaining for authentication and logging
- JSON responses use proper HTTP status codes
- Context propagation through request lifecycle
- Graceful shutdown handling with proper cleanup

## Web UI (`static/ui/index.html`)
- Single-page app using **Pico CSS v2** (classless/semantic, auto dark mode)
- Served via `//go:embed ui/*` in `static/static.go` — new files auto-included
- Pico dark mode: `data-theme` must be **absent** for auto (NOT set to `"auto"`)
- Semantic color vars: `--pico-ins-color` (green), `--pico-del-color` (red), `--pico-mark-background-color` (yellow)
- All user data must be escaped via `escapeHtml()` before innerHTML insertion
- Use `data-*` attributes + event delegation, not inline `onclick`
- Docker logs may contain ASCII control characters — clean them in the UI with `stripControlChars()`; stdout/stderr stream demuxing is handled server-side via `stdcopy.StdCopy` in the Docker adapter.

## CSP headers (`middleware.go`)
- `script-src 'self' 'unsafe-inline'` — inline scripts in index.html
- `style-src 'self' 'unsafe-inline'` — inline styles in index.html
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