# Running Tests

Unit tests can be run without Docker. Integration tests require either a running
Docker daemon or the Docker test server.

## Test Architecture

The test suite follows a 3-tier model:

| Tier | Command | Build Tag | Characteristics |
|------|---------|-----------|-----------------|
| **Unit** | `go test ./...` | None | Fast, mocked, deterministic time |
| **Integration** | `go test -tags=integration ./...` | `integration` | Real wiring, still mocked Docker |
| **Docker/E2E** | `go test -tags=docker ./...` | `docker` | Requires Docker daemon |

### Test Utilities

Shared test utilities are available in `test/testutil/`:

- **Eventually**: Poll a condition until it returns true or timeout
- **Never**: Assert a condition never becomes true
- **WaitForChan**: Wait for a channel with timeout
- **WaitForClose**: Wait for a channel to close

Example:
```go
import "github.com/netresearch/ofelia/test/testutil"

func TestServerStartup(t *testing.T) {
    srv := startServer()
    testutil.Eventually(t, func() bool {
        return srv.IsReady()
    }, testutil.WithTimeout(2*time.Second))
}
```

### Clock Interface

For deterministic time-based tests, use the `Clock` interface from `core/`:

```go
import "github.com/netresearch/ofelia/core"

func TestScheduler(t *testing.T) {
    clock := core.NewFakeClock(time.Now())
    // ... inject clock into scheduler
    
    clock.Advance(1 * time.Hour)  // Instant!
    // ... assert job ran
}
```

## Unit tests

```sh
go test ./...
```

## Integration tests

Start Docker and run the suite with the `integration` build tag:

```sh
go test -tags=integration ./...
```

The CI workflow executes unit tests first and runs the integration tests in a
separate step.

## Mutation Testing

Mutation testing measures test quality by introducing small changes (mutations)
to the code and checking if tests detect them. This helps identify weak tests
that pass but don't actually verify behavior.

### What is Mutation Testing?

- **Killed mutants**: Tests caught the mutation (good!)
- **Survived mutants**: Tests missed the mutation (needs improvement)
- **Mutation score**: Percentage of killed mutants (higher is better)

### Running Mutation Tests

We use [Gremlins](https://github.com/go-gremlins/gremlins) for mutation testing:

```sh
# Install Gremlins
go install github.com/go-gremlins/gremlins/cmd/gremlins@latest

# Run full mutation testing (unit tests only)
gremlins unleash --config=.gremlins.yaml

# Run diff-based mutation testing (only changed files)
gremlins unleash --config=.gremlins.yaml --diff

# Run Docker adapter mutation testing with integration tests
# Requires Docker daemon - takes ~10 minutes
gremlins unleash --config=.gremlins-docker.yaml
```

### Docker Adapter Mutation Testing

The Docker adapter package (`core/adapters/docker`) benefits significantly from
integration tests during mutation testing. A separate config file is provided:

```sh
# Run with integration tests (requires Docker daemon)
# Note: --tags must be passed on command line as YAML tags field is not respected
gremlins unleash ./core/adapters/docker --config=.gremlins-docker.yaml --tags integration
```

**Results comparison:**
- Without integration tests: ~50% test efficacy
- With integration tests: ~69% test efficacy

Integration tests exercise real Docker SDK paths that unit tests with mocks
cannot fully cover. The integration test config uses higher timeouts since each
mutation requires running tests that connect to the Docker daemon.

### Configuration

The `.gremlins.yaml` configuration defines:

- **Test packages**: cli, config, core, logging, metrics, middlewares, web
- **Mutators enabled**:
  - `CONDITIONALS_BOUNDARY` - Change `<` to `<=`, `>` to `>=`, etc.
  - `CONDITIONALS_NEGATION` - Negate conditions (`==` to `!=`)
  - `INCREMENT_DECREMENT` - Change `++` to `--` and vice versa
  - `INVERT_LOGICAL` - Invert `&&` to `||`, `||` to `&&`
  - `INVERT_NEGATIVES` - Remove negation operators
  - `INVERT_LOOPCTRL` - Change `break` to `continue` and vice versa
- **Threshold**: 60% mutation score required
- **Coverage-aware**: Only mutates code covered by tests

### CI Integration

Mutation testing runs automatically:

- **Weekly**: Full mutation testing every Sunday at 2 AM UTC
- **PRs**: Diff-based testing on Go file changes
- **Manual**: Can be triggered via workflow_dispatch

Results are posted as PR comments and uploaded as artifacts (JSON/HTML reports).

### Improving Mutation Score

When mutants survive, consider:

1. **Add edge case tests**: Test boundary conditions
2. **Strengthen assertions**: Be more specific in what you verify
3. **Test error paths**: Ensure error handling is tested
4. **Check conditional logic**: Verify both branches of conditions
