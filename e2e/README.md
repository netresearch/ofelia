# End-to-End (E2E) Tests

## Overview

E2E tests verify the complete Ofelia scheduler system behavior in real scenarios with actual Docker containers.

## Running E2E Tests

```bash
# Run all E2E tests
go test -tags=e2e -v ./e2e/

# Run specific test
go test -tags=e2e -v -run TestScheduler_BasicLifecycle ./e2e/

# Run with timeout
go test -tags=e2e -v -timeout 5m ./e2e/
```

## Test Coverage

### Scheduler Lifecycle Tests
- **TestScheduler_BasicLifecycle**: Tests start, schedule, execute, stop cycle with real Docker containers
- **TestScheduler_MultipleJobsConcurrent**: Tests concurrent job execution (3 jobs running simultaneously)
- **TestScheduler_JobFailureHandling**: Tests failure resilience and error handling

These E2E tests provide comprehensive coverage of scheduler lifecycle behavior that was previously attempted with flaky unit tests using mocks. Real Docker integration provides more reliable and realistic testing.

## Prerequisites

- Docker daemon running
- `alpine:latest` image available
- Network connectivity to Docker socket

## Test Structure

Each E2E test follows this pattern:
1. Setup: Create test containers
2. Configure: Create jobs and scheduler
3. Execute: Start scheduler and let jobs run
4. Verify: Check execution history and results
5. Cleanup: Stop scheduler and remove containers

## Future E2E Tests

Planned but not yet implemented:
- Config reload scenarios (SIGHUP handling)
- Label-based job discovery
- Multi-container workflows
- Job dependency chains
- Stress tests for concurrent execution
