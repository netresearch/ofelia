# Contributing to Ofelia

Thank you for your interest in contributing to Ofelia! This document provides guidelines for contributing to the project.

## Table of Contents

- [Developer Certificate of Origin (DCO)](#developer-certificate-of-origin-dco)
- [Development Setup](#development-setup)
- [Testing Strategy](#testing-strategy)
- [Code Style](#code-style)
- [Pull Request Process](#pull-request-process)
- [Release Process](#release-process)

## Developer Certificate of Origin (DCO)

All contributions to Ofelia must include a `Signed-off-by` trailer in the
commit message, certifying that you wrote the code or have the right to submit
it under the project's open-source license. This is the
[Developer Certificate of Origin (DCO)](https://developercertificate.org/).

### How to sign off

Add `--signoff` (or `-s`) to your `git commit` command:

```bash
git commit --signoff -m "feat(core): add new scheduler algorithm"
```

This appends a line like:

```
Signed-off-by: Your Name <your.email@example.com>
```

If you have already made commits without sign-off, you can amend or rebase to
add it:

```bash
# Amend the last commit
git commit --amend --signoff --no-edit

# Rebase and sign off all commits on your branch
git rebase --signoff HEAD~N   # where N is the number of commits
```

### Automated enforcement

The project's lefthook `commit-msg` hook validates the presence of
`Signed-off-by` locally. CI also checks all commits in a pull request via the
[DCO GitHub App](https://github.com/apps/dco).

## Development Setup

### Prerequisites

- Go 1.27 or higher
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

### Security decisions that fall back

Some code picks a security setting from what it finds at run time, for example "no config file, so the unauthenticated web UI stays off". For such code, a review lists every class of input the decision can see and states which way the decision goes for each class. Each class that leads to the less safe setting needs a reason. A test covers each class.

The classes that #981 needed for a config path show the depth this takes:

- the path does not exist
- the path exists but cannot be read
- the path is a glob, and a match is a dangling symlink
- the path is a glob in a directory part, or in a directory the process cannot list
- the path is a directory
- the same path on Windows, where `\` separates directories and does not escape a glob character

The Go behaviours behind these cases are in the go-development skill, `references/filepath-globs.md`.

## Pull Request Process

1. **Fork and create a branch**: Create a feature branch from `main`
2. **Write tests**: Add tests for new functionality (unit + integration if needed)
3. **Run tests**: Verify all tests pass locally
4. **Update documentation**: Update README.md, docs/, or CONTRIBUTING.md as needed
5. **Create PR**: Write clear description of changes and why they're needed
6. **Address feedback**: Respond to review comments and make requested changes

### PR Checklist

- [ ] All commits are signed off (`git commit --signoff`) per [DCO](#developer-certificate-of-origin-dco)
- [ ] Tests added for new functionality
- [ ] All tests pass (`go test -tags=integration,e2e -v ./...`)
- [ ] Code follows project style guidelines
- [ ] Documentation updated (if needed)
- [ ] Changelog fragment added under `changelog.d/` for a change users notice (see [changelog.d/README.md](changelog.d/README.md)), not an edit of `CHANGELOG.md`
- [ ] Commit messages are clear and descriptive
- [ ] No breaking changes (or clearly documented if unavoidable)

## Release Process

### For Maintainers

Ofelia uses [SLSA Level 3](https://slsa.dev/) provenance for all releases, providing cryptographic guarantees about the build process.

#### Creating a Release

The version comes from the git tag. Pushing the tag is the release: CI creates
the GitHub release, so never create it by hand (`gh release create` or the web
form). Under immutable releases a release created that way burns the version.

1. **Open the release pull request.** Run `make changelog-collect`, which moves
   every `changelog.d/` fragment into `## [Unreleased]` of `CHANGELOG.md`, then
   rename `## [Unreleased]` to the version and add a new empty
   `## [Unreleased]` above it.

2. **Merge it, and tag the merge commit on `main`:**
   ```bash
   git fetch origin
   git switch --detach origin/main      # the merged release commit
   git tag -s vX.Y.Z -m "Release vX.Y.Z"
   git push origin vX.Y.Z
   ```

3. **Wait for the pipeline.** The release workflow then:
   - Creates the GitHub release
   - Builds binaries with SLSA Level 3 provenance
   - Generates SBOMs for all artifacts
   - Creates signed checksums (Cosign keyless)
   - Builds and signs container images
   - Updates release notes with verification instructions

   `gh release edit vX.Y.Z --notes-file notes.md` replaces the generated notes
   afterwards.

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
# Verify a binary against its bundled signature
cosign verify-blob \
  --bundle ofelia-linux-amd64.sigstore.json \
  --certificate-identity-regexp "^https://github\.com/netresearch/\.github/\.github/workflows/release-go-app\.yml@" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  ofelia-linux-amd64

# Verify build provenance
gh attestation verify ofelia-linux-amd64 \
  --repo netresearch/ofelia \
  --signer-workflow netresearch/.github/.github/workflows/release-go-app.yml
```

The signing identity is the reusable workflow in `netresearch/.github`, not
this repository — see [SECURITY.md](SECURITY.md) for the full procedure.

## Questions or Issues?

- Open an issue for bugs or feature requests
- Discuss major changes before implementing
- Ask questions in pull request comments

Thank you for contributing to Ofelia!
