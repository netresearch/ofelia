<!-- Managed by agent: keep sections and order; edit content, not structure. Last updated: 2025-09-29 -->

# AGENTS.md — Middlewares (Notifications & Processing)

## Overview
- Notification and processing middleware for job execution
- Main components: `mail.go` (email), `slack.go` (Slack), `save.go` (file output)
- Additional: `overlap.go` (job overlap prevention), `sanitize.go` (output cleaning)
- Middleware chain processing for job lifecycle events

## Setup & environment
- Install: `go mod download`
- Test middlewares: `go test ./middlewares/...`
- Environment: email/Slack credentials needed for integration tests
- SMTP configuration required for mail middleware

## Build & tests (prefer file-scoped)
- Typecheck package: `go build ./middlewares`
- Lint file: `golangci-lint run ./middlewares/mail.go`
- Format file: `gofmt -w ./middlewares/mail.go`
- Run tests for this package: `go test ./middlewares/...`
- Integration tests: `go test -tags=integration ./middlewares/...`

## Code style & conventions
- Use `core.Logger` for all logging, never direct `log` package
- Middleware interface: implement common patterns for extensibility
- Configuration: use structured config with validation
- Error handling: graceful degradation when notifications fail
- Template processing: secure template rendering with proper escaping
- Rate limiting: prevent notification spam

## Security & safety
- Credentials: never log API keys, tokens, or passwords
- Template injection: sanitize all user input in templates
- Network requests: use timeouts, validate SSL certificates
- Email security: validate recipient addresses, prevent header injection
- Slack integration: validate webhook URLs, use appropriate scopes
- File operations: validate file paths, proper permissions

## PR/commit checklist
- [ ] All tests pass: `go test ./middlewares/...`
- [ ] Integration tests pass (with appropriate credentials)
- [ ] No secrets in logs or error messages
- [ ] Template rendering is secure
- [ ] Network operations have timeouts
- [ ] Error handling gracefully degrades

## Good vs. bad examples
- Good: `mail.go` (secure email handling with templates)
- Good: `overlap.go` (job coordination patterns)
- Good: `sanitize.go` (safe output processing)
- Bad: Hardcoded credentials or API keys
- Bad: Template injection vulnerabilities

## When stuck
- Review notification patterns in `mail.go` and `slack.go`
- Check job overlap prevention logic in `overlap.go`
- Look at output sanitization in `sanitize.go`
- Reference common middleware utilities in `common.go`