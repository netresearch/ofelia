<!-- Managed by agent: keep sections and order; edit content, not structure. Last updated: 2025-09-29 -->

# AGENTS.md — CLI (Command-Line Interface)

## Overview
- Command-line interface and configuration management for Ofelia
- Main entry points: `daemon.go`, `config.go`, `validate.go`
- Handles Docker label parsing, configuration initialization, and validation
- Entry point: run `go build -o ofelia ofelia.go` from root, then `./ofelia daemon`

## Setup & environment
- Install: `go mod download`
- Run daemon: `./ofelia daemon --config=example/config.ini`
- Test CLI: `go test ./cli/...`
- Environment: requires Docker daemon running for integration tests

## Build & tests (prefer file-scoped)
- Typecheck package: `go build ./cli`
- Lint file: `golangci-lint run ./cli/config.go`
- Format file: `gofmt -w ./cli/config.go`
- Run tests for this package: `go test ./cli/...`
- Integration tests: `go test -tags=integration ./cli/...`

## Code style & conventions
- Use structured logging via `core.Logger` (logrus adapter), never `log` package
- Configuration structs use `mapstructure` tags for INI mapping
- Docker integration follows Docker API best practices
- Error handling: wrap errors with context using `fmt.Errorf`
- Validation: comprehensive validation with clear error messages

## Security & safety
- Never log sensitive configuration values (passwords, tokens)
- Docker socket access requires appropriate permissions
- Configuration files should not contain secrets in production
- Input validation on all configuration parameters

## PR/commit checklist
- [ ] All tests pass: `go test ./cli/...`
- [ ] Integration tests pass (if Docker-related): `go test -tags=integration ./cli/...`
- [ ] Configuration changes are backward compatible
- [ ] Error messages are user-friendly
- [ ] Docker label parsing handles edge cases

## Good vs. bad examples
- Good: `config.go` (structured config with validation)
- Good: `validate.go` (comprehensive input validation)
- Bad: Direct `log` package usage (use `core.Logger` instead)

## When stuck
- Check `example/config.ini` for configuration format examples
- Look at existing tests for Docker integration patterns
- Validate against CI requirements in `.github/workflows/ci.yml`