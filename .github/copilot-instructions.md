# Ofelia - Job Scheduler for Docker

This is a Go-based job scheduler for Docker containers. Ofelia orchestrates container tasks with minimal overhead, offering a sleek alternative to cron. It supports multiple job types (exec, run, local, service-run, compose) and includes a web UI for job management.

## Code Standards

### Required Before Each Commit
- Run `gofmt -w` on all modified Go files before committing. Unformatted code must not be committed.
- Use semantic commit messages following the Conventional Commits style (e.g., `feat:`, `fix:`, `docs:`)
- Write comprehensive commit message bodies that thoroughly describe every change introduced

### Development Flow
- **Format code**: `make fmt` or `gofmt -w $(git ls-files '*.go')`
- **Check formatting**: `make fmt-check` (fails CI if files are not formatted)
- **Vet code**: `make vet` or `go vet ./...` (must pass before committing)
- **Run linter**: `make lint` (runs golangci-lint with 45+ rules)
- **Fix linting issues**: `make lint-fix` (auto-fixes many issues)
- **Security check**: `make security-check` (runs gosec)
- **Run tests**: `make test` or `go test ./...` (all tests must pass)
- **Test coverage**: `make test-coverage` (minimum 60% coverage maintained)
- **Full CI check**: `make lint-full` (runs vet, fmt-check, lint, security-check)

### Pre-commit Hooks
The project uses Git pre-commit hooks that run:
- `go vet` - Static analysis for common Go errors
- `gofmt` - Go code formatting validation
- `golangci-lint` - Comprehensive linting
- `gosec` - Security vulnerability scanning
- Secret detection - Basic check for hardcoded credentials

Install hooks with: `./scripts/install-hooks.sh`

## Repository Structure

- **`cli/`**: Command-line interface and argument parsing
- **`config/`**: Configuration loading and validation from INI files and Docker labels
- **`core/`**: Core job scheduling engine, job execution, and cron integration
- **`web/`**: HTTP API and web UI server (embedded static files)
- **`logging/`**: Logging middleware (mail, Slack, save-to-disk)
- **`metrics/`**: Prometheus metrics integration
- **`middlewares/`**: Execution middlewares for job lifecycle
- **`static/`**: Web UI static assets (embedded in binary)
- **`test/`**: Test helpers and fixtures
- **`docs/`**: Documentation including architecture, API, jobs reference
- **`example/`**: Example Docker Compose setups demonstrating job types

## Key Guidelines

1. **Go Best Practices**: Follow idiomatic Go patterns and conventions
2. **Maintain Structure**: Keep existing code organization and architecture
3. **Docker Integration**: The app heavily relies on Docker client; many tests require Docker
4. **Configuration Sources**: Config can come from INI files, Docker labels, env vars, or CLI flags (in that precedence order)
5. **Job Types**: Understand the five job types (exec, run, local, service-run, compose) when making changes
6. **Web UI**: The web UI uses embedded static files; rebuild binary after changing static assets
7. **Graceful Shutdown**: The app implements graceful shutdown with configurable timeout
8. **Middleware Pattern**: Job execution uses middleware for logging, metrics, and error handling

## Testing Requirements

- Tests must pass: `make test` or `go test ./...`
- Some tests require Docker to be running (especially core and config tests)
- Maintain test coverage above 60%: `make test-coverage`
- Add tests for new features and bug fixes
- Follow existing test patterns in the codebase

## Documentation

- Update `README.md` when changing user-facing behavior
- Update docs in `docs/` folder when changing architecture or configuration
- API changes should be reflected in `docs/API.md` and `docs/openapi.yaml`
- Job configuration changes should update `docs/jobs.md`

## Repository Hygiene

- **Dependencies**: Manage exclusively with Go modules (`go.mod` and `go.sum`)
- **Never vendor**: Do NOT run `go mod vendor` or commit the `vendor/` directory
- **Module tidying**: Run `make tidy` or `go mod tidy` after dependency changes
- **`.gitignore`**: Ensure build artifacts, binaries, and temporary files are excluded

## Build and Release

- **Local build**: `go build .` or `make build`
- **Cross-platform**: `make packages` (builds for darwin and linux amd64)
- **Docker image**: `make docker-build`
- **Version info**: Embeds Git branch and build timestamp via ldflags

## Common Tasks

- **Add new job type**: Modify `core/` package, update config parsing in `config/`, update docs
- **Add middleware**: Implement in `middlewares/` or `logging/`, wire up in core job runner
- **Modify web UI**: Update files in `static/`, rebuild binary to embed changes
- **Add configuration option**: Update INI parser, Docker label parser, and CLI flags
- **Change scheduling**: Modify cron integration in `core/` (uses robfig/cron/v3)

## Security Considerations

- Never commit secrets or credentials
- Validate all external input (config files, Docker labels, API requests)
- Use `gosec` to scan for security vulnerabilities: `make security-check`
- Handle Docker socket access carefully (read-only when possible)
- Sanitize job commands before execution
- Implement proper authentication for web UI when enabled
