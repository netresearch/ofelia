<!-- Managed by agent: keep sections and order; edit content, not structure. Last updated: 2025-09-29 -->

# AGENTS.md — Core (Business Logic & Scheduling)

## Overview
- Core business logic, job scheduling, and execution engine
- Main entry points: `scheduler.go`, `runservice.go`, `workflow.go`
- Job types: `runjob.go` (Docker containers), `localjob.go` (local execution), `execjob.go` (exec in containers)
- Cron scheduling via `github.com/netresearch/go-cron` (fork of robfig/cron with panic fixes)

## Setup & environment
- Install: `go mod download`
- Test core: `go test ./core/...`
- Integration tests: `go test -tags=integration ./core/...`
- Environment: Docker daemon required for container job tests

## Build & tests (prefer file-scoped)
- Typecheck package: `go build ./core`
- Lint file: `golangci-lint run ./core/scheduler.go`
- Format file: `gofmt -w ./core/scheduler.go`
- Run tests for this package: `go test ./core/...`
- Benchmark tests: `go test -bench=. ./core/...`
- Race detection: `go test -race ./core/...`

## Code style & conventions
- Use `*slog.Logger` from stdlib `log/slog` for all logging
- Context propagation: always pass context through job execution
- Error handling: wrap with contextual information, use custom error types
- Concurrency: proper sync primitives, avoid race conditions
- Resource cleanup: always defer cleanup, use buffer pools for efficiency
- Time handling: use `time.Time` and proper timezone handling

## Security & safety
- Container isolation: never execute untrusted code without proper sandboxing  
- Resource limits: enforce timeouts and resource constraints on jobs
- Secret handling: never log secrets, use secure credential passing
- Network access: validate container network configurations
- File system access: validate mount points and permissions

## PR/commit checklist
- [ ] All unit tests pass: `go test ./core/...`
- [ ] Integration tests pass: `go test -tags=integration ./core/...`
- [ ] Race conditions tested: `go test -race ./core/...`
- [ ] Resource cleanup verified (no goroutine/memory leaks)
- [ ] Error handling covers failure scenarios
- [ ] Concurrency safety validated

## Good vs. bad examples
- Good: `scheduler.go` (proper concurrency and error handling)
- Good: `resilient_job.go` (retry logic with exponential backoff)
- Good: `buffer_pool.go` (efficient resource management)
- Bad: Missing context propagation
- Bad: Blocking operations without timeouts

## When stuck
- Review existing job implementations (`runjob.go`, `localjob.go`, `execjob.go`)
- Check cron expression handling in `cron_utils.go`
- Look at resilience patterns in `resilience.go` and `retry.go`
- Validate Docker integration patterns in `docker_client.go`