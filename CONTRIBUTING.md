# Contributing to Ofelia

Thank you for your interest in contributing to Ofelia! This document provides guidelines for contributing to the project.

## Table of Contents

- [Development Setup](#development-setup)
- [Testing Strategy](#testing-strategy)
- [Code Style](#code-style)
- [Pull Request Process](#pull-request-process)
- [Release Process](#release-process)

## Development Setup

### Prerequisites

- Go 1.25 or higher
- Docker (for integration and E2E tests)
- Docker Swarm enabled (for service job tests)

### Building

```bash
go build -o ofelia .
```

### Running Locally

```bash
./ofelia daemon --config config.ini
```

## Testing Strategy

Ofelia uses a multi-layered testing approach following the testing pyramid:

### Test Pyramid

```
       E2E Tests (e2e/)
    Integration Tests (integration tag)
  Unit Tests (no build tags)
```

### Test Categories

#### 1. Unit Tests

**Location**: `core/*_test.go` (files without build tags)
**Coverage Target**: 65%+
**Run Command**: `go test -v ./core/`

Unit tests verify individual components in isolation using mocks where needed. They should:
- Be fast (<100ms per test)
- Not require external dependencies (Docker, network, etc.)
- Use mocks for Docker client when testing job logic
- Focus on business logic, error handling, and edge cases

**Example**:
```bash
# Run unit tests only
go test -v ./core/

# Run with coverage
go test -v -coverprofile=coverage.out ./core/
go tool cover -func=coverage.out
```

#### 2. Integration Tests

**Location**: `core/*_test.go` (files with `//go:build integration` tag)
**Coverage Target**: Critical paths for Docker integration
**Run Command**: `go test -tags=integration -v ./core/`

Integration tests verify interaction with real Docker daemon. They should:
- Require Docker daemon running
- Use real containers (alpine:latest for simplicity)
- Test actual Docker API behavior
- Clean up containers after test completion

**Swarm Requirements**: Some tests require Docker Swarm to be initialized:
```bash
docker swarm init
go test -tags=integration -v ./core/
```

**Example**:
```bash
# Run integration tests
go test -tags=integration -v ./core/

# Run specific integration test
go test -tags=integration -v -run TestExecJob_WorkingDir_Integration ./core/
```

#### 3. End-to-End (E2E) Tests

**Location**: `e2e/` directory (files with `//go:build e2e` tag)
**Coverage Target**: Complete system behavior scenarios
**Run Command**: `go test -tags=e2e -v ./e2e/`

E2E tests verify complete Ofelia system behavior with actual containers and scheduler. They should:
- Test scheduler lifecycle (start, execute, stop)
- Verify concurrent job execution
- Validate failure resilience
- Use real Docker containers and scheduler instances
- Take longer to run (5-30 seconds per test)

**Example**:
```bash
# Run all E2E tests
go test -tags=e2e -v ./e2e/

# Run specific E2E test
go test -tags=e2e -v -run TestScheduler_BasicLifecycle ./e2e/

# Run with timeout for long-running tests
go test -tags=e2e -v -timeout 5m ./e2e/
```

### Running All Tests

```bash
# Unit tests only (fast, no Docker required)
go test -v ./...

# Unit + Integration tests (requires Docker)
go test -tags=integration -v ./...

# All tests including E2E (requires Docker)
go test -tags=e2e,integration -v ./...
```

### Test Coverage

View coverage report:
```bash
# Generate coverage for unit tests
go test -coverprofile=coverage.out ./core/
go tool cover -html=coverage.out

# Generate coverage including integration tests
go test -tags=integration -coverprofile=coverage-integration.out ./core/
go tool cover -html=coverage-integration.out
```

**Coverage Goals**:
- Core package unit tests: 65%+
- Core package with integration tests: 70%+
- Focus on error paths and edge cases
- 100% coverage not required for test helpers

### Writing New Tests

#### Unit Test Example

```go
func TestJobNameValidation(t *testing.T) {
    job := &ExecJob{}
    job.Name = ""

    if err := job.Validate(); err == nil {
        t.Error("Expected error for empty job name")
    }
}
```

#### Integration Test Example

```go
//go:build integration
// +build integration

package core

func TestExecJob_RealDocker_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }

    client, err := docker.NewClient("unix:///var/run/docker.sock")
    if err != nil {
        t.Skip("Docker not available, skipping integration test")
    }

    // ... test implementation with real Docker
}
```

#### E2E Test Example

```go
//go:build e2e
// +build e2e

package e2e

func TestScheduler_NewFeature(t *testing.T) {
    // 1. Setup: Create test containers
    // 2. Configure: Create jobs and scheduler
    // 3. Execute: Start scheduler and let jobs run
    // 4. Verify: Check execution history and results
    // 5. Cleanup: Stop scheduler and remove containers
}
```

### Test Best Practices

1. **Use descriptive test names**: `TestExecJob_WorkingDir_UsesContainerDefault`
2. **Clean up resources**: Always use `defer` for cleanup (containers, files)
3. **Skip when dependencies unavailable**: Check Docker availability, skip if not present
4. **Avoid flaky tests**: Don't rely on precise timing, use synchronization primitives
5. **Test error paths**: Don't just test happy paths, verify error handling
6. **Keep tests focused**: One test should verify one behavior
7. **Use table-driven tests**: For testing multiple similar scenarios

### CI/CD Integration

Tests run automatically on:
- Pull requests (unit tests)
- Main branch commits (unit + integration tests)
- Release tags (all tests including E2E)

## Code Style

### General Guidelines

- Follow standard Go conventions (`gofmt`, `golint`)
- Use meaningful variable and function names
- Add comments for exported functions and types
- Keep functions focused and small
- Handle errors explicitly, don't ignore them

### Docker Integration

- Always use absolute container IDs, not names (names can conflict)
- Clean up containers in `defer` statements
- Use `alpine:latest` for test containers (small, fast)
- Check container status before operations

### Error Handling

```go
// Good: Explicit error handling
if err := job.Run(ctx); err != nil {
    return fmt.Errorf("job run: %w", err)
}

// Bad: Ignoring errors
job.Run(ctx)
```

## Pull Request Process

1. **Fork and create a branch**: Create a feature branch from `main`
2. **Write tests**: Add tests for new functionality (unit + integration if needed)
3. **Run tests**: Verify all tests pass locally
4. **Update documentation**: Update README.md, docs/, or CONTRIBUTING.md as needed
5. **Create PR**: Write clear description of changes and why they're needed
6. **Address feedback**: Respond to review comments and make requested changes

### PR Checklist

- [ ] Tests added for new functionality
- [ ] All tests pass (`go test -tags=integration,e2e -v ./...`)
- [ ] Code follows project style guidelines
- [ ] Documentation updated (if needed)
- [ ] Commit messages are clear and descriptive
- [ ] No breaking changes (or clearly documented if unavoidable)

## Release Process

### For Maintainers

Ofelia uses [SLSA Level 3](https://slsa.dev/) provenance for all releases, providing cryptographic guarantees about the build process.

#### Creating a Release

Version is automatically derived from the git tag by GoReleaser.

1. **Create signed tag** (recommended for GPG-signed releases):
   ```bash
   # Configure GPG signing (one-time setup)
   git config --global user.signingkey YOUR_GPG_KEY_ID
   git config --global tag.gpgSign true

   # Create signed tag
   git tag -s v0.X.Y -m "Release v0.X.Y"
   git push origin v0.X.Y
   ```

2. **Create GitHub Release**: Go to [Releases](https://github.com/netresearch/ofelia/releases) → Draft a new release
   - Select the tag you just created
   - Generate release notes or write a summary
   - Publish the release

3. **Automated pipeline**: The release workflow automatically:
   - Builds binaries with SLSA Level 3 provenance
   - Generates SBOMs for all artifacts
   - Creates signed checksums (Cosign keyless)
   - Builds and signs container images
   - Updates release notes with verification instructions

#### Supply Chain Security

All releases include:

| Artifact | Verification |
|----------|-------------|
| Binaries | SLSA L3 provenance attestation |
| Checksums | Cosign signature + certificate |
| Containers | Cosign signature + SBOM |
| Source | Signed git tag (if created with `-s`) |

#### Verifying Releases

Users can verify releases:

```bash
# Verify binary provenance
slsa-verifier verify-artifact ofelia-linux-amd64 \
  --provenance-path ofelia-linux-amd64.intoto.jsonl \
  --source-uri github.com/netresearch/ofelia

# Verify checksums signature
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity "https://github.com/netresearch/ofelia/.github/workflows/release-slsa.yml@refs/tags/vX.Y.Z" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
```

## Questions or Issues?

- Open an issue for bugs or feature requests
- Discuss major changes before implementing
- Ask questions in pull request comments

Thank you for contributing to Ofelia!
