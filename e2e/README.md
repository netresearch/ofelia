# End-to-End (E2E) Tests

## Overview

E2E tests verify Ofelia's behavior at the process boundary: they build the
real `ofelia` binary, spawn it as a child process with an INI config, and
assert on its stdout/stderr, file system side-effects and, for Docker jobs,
on real container state. They complement the `integration` tests (which run
in-process with the Docker SDK wired directly) by exercising the full
pipeline:

```
parse INI → configure scheduler → fire tick → spawn job → collect output →
handle signal → graceful shutdown
```

## Running

```bash
# All e2e tests
go test -tags=e2e -race -v ./e2e/...

# Single test
go test -tags=e2e -race -v -run TestE2E_LocalJob_RunsOnSchedule ./e2e/

# Increase timeout for slow CI runners
go test -tags=e2e -race -v -timeout=10m ./e2e/...
```

## Prerequisites

- Go toolchain matching `go.mod`
- Docker daemon (Docker tests skip automatically when unavailable)
- `alpine:3.20` image is pulled on demand by the Docker tests
- Chrome or Chromium for the web-UI test (skips automatically when absent).
  Any of `google-chrome`, `google-chrome-stable`, `chromium` or
  `chromium-browser` on `PATH` is used; set `OFELIA_E2E_CHROME=/path/to/chrome`
  to point at a browser that is not on `PATH`.

## What is covered

### Binary-level scheduling
- `TestE2E_LocalJob_RunsOnSchedule` — schedule a local-exec job, assert it
  fires at least once and its side-effect (file marker) is visible.
- `TestE2E_LocalJob_RunOnStartup` — `run-on-startup = true` fires immediately
  without waiting for the cron tick.
- `TestE2E_LocalJob_SurvivesMultipleExecutions` — scheduler stays healthy
  under repeated fast-cadence ticks.

### Real Docker execution
- `TestE2E_DockerRunJob_SpawnsContainer` — pulls alpine, runs a real
  container, verifies the marker via `docker logs`.
- `TestE2E_DockerRunJob_FailingContainerMarkedFailed` — non-zero container
  exit is surfaced as `failed: true` in Ofelia's log.

### Web UI (real browser)
- `TestE2E_WebUI_ExpandedOutputSurvivesRefresh` — drives the served UI in
  headless Chrome: expand a run's output, outlast the 5s auto-refresh, assert
  it is still open. Regression guard for
  [#764](https://github.com/netresearch/ofelia/issues/764), where the refresh
  re-rendered the history table and collapsed it.

### Configuration surface
- `TestE2E_Validate_MalformedINI` — malformed INI produces a useful error.
- `TestE2E_Validate_MissingConfigFile` — missing file path is reported.
- `TestE2E_Validate_AcceptsValidConfig` — happy path, structured JSON dump
  includes declared jobs.

### Signal handling
- `TestE2E_GracefulShutdown_SIGTERM` — SIGTERM during a scheduling window
  produces a clean exit; shutdown banner is logged.
- `TestE2E_GracefulShutdown_SIGINT` — SIGINT (Ctrl+C) path covered
  independently of SIGTERM.

### Legacy (mock-backed) lifecycle tests
- `TestScheduler_BasicLifecycle` / `TestScheduler_MultipleJobsConcurrent` /
  `TestScheduler_JobFailureHandling` in `scheduler_lifecycle_test.go` —
  use an in-process mock Docker provider for fast feedback on scheduler
  state-machine behavior. Kept as-is for backwards compatibility.

## Out of scope

Deliberately *not* covered by e2e (owned by integration or unit tests):
- Job types that need a running compose stack (`job-compose`).
- Swarm-only `job-service-run` (requires a Swarm manager).
- Docker label discovery (owned by `cli/docker_handler_integration_test.go`).
- Web UI auth flows (owned by `web/` tests).
- Full config-reload/SIGHUP semantics.

## How the harness works

`helpers_test.go` exposes a small set of utilities:

| Helper | Purpose |
|--------|---------|
| `buildBinary(t)` | `go build -race` once per test process, cached. |
| `startDaemon(t, configPath)` | Spawn `ofelia daemon --config=...`, wait for boot banner. |
| `daemonProcess.waitForLog` | Poll captured stdout for a substring with timeout. |
| `daemonProcess.shutdown` | SIGTERM + wait, SIGKILL on timeout. |
| `runCommand(t, args...)` | One-shot command invocation for `validate`-style tests. |
| `dockerAvailable(t)` / `dockerLogs` / `dockerRemove` | Thin `docker` CLI wrappers. |

Every test uses `t.TempDir()` for config files and marker outputs, so parallel
execution is safe (tests are `t.Parallel()` wherever possible).
