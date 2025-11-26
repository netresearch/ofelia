<!-- Managed by agent: keep sections and order; edit content, not structure. Last updated: 2025-09-29 -->

# AGENTS.md — Test (Testing Utilities & Integration)

## Overview
- Testing utilities, integration test setup, and test data
- Main components: `testlogger.go` (test logging utilities)
- Test configuration: `test-config.ini` (test environment config)
- Integration test data: `run-job/` directory with test job definitions

## Setup & environment
- Install: `go mod download`
- Run tests: `go test ./test/...`
- Integration tests: `go test -tags=integration ./...`
- Environment: Docker daemon required for integration tests

## Build & tests (prefer file-scoped)
- Typecheck package: `go build ./test`
- Lint file: `golangci-lint run ./test/testlogger.go`
- Format file: `gofmt -w ./test/testlogger.go`
- Run tests for this package: `go test ./test/...`
- All integration tests: `go test -tags=integration ./...`

## Code style & conventions
- Test helpers: reusable utilities for consistent test setup
- Mock objects: prefer dependency injection for testability
- Test data: use deterministic, reproducible test scenarios
- Cleanup: proper teardown of test resources
- Assertions: clear, descriptive test failure messages
- Test organization: follow Go test naming conventions

## Security & safety
- Test isolation: ensure tests don't interfere with each other
- Test data: avoid sensitive data in test configurations
- Resource cleanup: prevent test resource leaks
- Docker tests: clean up containers and volumes after tests
- Network tests: avoid external dependencies where possible

## PR/commit checklist
- [ ] All unit tests pass: `go test ./...`
- [ ] Integration tests pass: `go test -tags=integration ./...`
- [ ] Test coverage maintained or improved
- [ ] New tests for new functionality
- [ ] Test data is appropriate and clean
- [ ] No test resource leaks

## Good vs. bad examples
- Good: `testlogger.go` (structured test logging utilities)
- Good: `test-config.ini` (isolated test configuration)
- Bad: Tests with external dependencies
- Bad: Tests that don't clean up resources
- Bad: Flaky tests with timing dependencies

## When stuck
- Check test configuration patterns in `test-config.ini`
- Review test logging utilities in `testlogger.go`
- Look at integration test setup in other packages
- Reference CI test configuration in `.github/workflows/ci.yml`