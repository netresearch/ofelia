# Development Guide

## Getting Started

### Prerequisites

- Go 1.25+
- Docker and Docker Compose
- Git
- **Recommended**: [direnv](https://direnv.net/) for automatic environment setup

### Development Setup

1. Clone the repository:
```bash
git clone https://github.com/netresearch/ofelia.git
cd ofelia
```

2. **Recommended**: Install direnv for automatic environment setup:
```bash
# Install direnv (see https://direnv.net/docs/installation.html)
# macOS: brew install direnv
# Ubuntu: sudo apt install direnv
# Then add to your shell config (.bashrc, .zshrc, etc.):
eval "$(direnv hook bash)"  # or zsh, fish, etc.

# Allow the .envrc file
direnv allow
```

The `.envrc` file automatically:
- ✅ Verifies Go 1.25+ installation
- ✅ Checks required tools (golangci-lint, gosec, docker)
- ✅ **Enforces Git hooks setup** - prevents commits without hooks
- ✅ Sets up development aliases and environment variables
- ✅ Provides auto-install option for Git hooks

3. Install Git hooks for linting enforcement (if not using direnv auto-install):

```bash
make setup
# or
./scripts/install-hooks.sh  # legacy wrapper
```

This installs **lefthook** (Go-native git hooks) with **fast parallel execution** (~4-6s typical), automatically running all linters before each commit to ensure code quality and consistency with the CI pipeline.

### Git Hooks

The project uses **lefthook** (Go-native) for fast, parallel git hooks to enforce code quality standards and development workflow best practices.

#### Pre-Commit (Quality Gates)

Runs automatically before each commit (~4-6s with parallel execution):

- **go mod tidy** - Dependency hygiene check
- **go vet** - Static analysis for common Go programming errors
- **gofmt** - Go code formatting validation
- **golangci-lint** - Comprehensive linting with 45+ enabled rules
- **golangci-lint (extra)** - Additional quality linters (gci, wrapcheck, etc.)
- **gosec** - Security vulnerability scanning
- **Secret detection** - Prevents hardcoded credentials in code files

#### Commit-Msg (Message Validation)

Validates commit message format:

- **Minimum length** - At least 10 characters required
- **Conventional commits** - Recommends `type(scope): message` format
  - Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `perf`, `ci`, `build`, `revert`
  - Example: `feat(core): add new scheduler algorithm`
- **WIP protection** - Blocks WIP commits to main/master branches

#### Pre-Push (Final Safety Check)

Runs before pushing to remote (~10-30s depending on test suite):

- **Full test suite** - Runs all tests with race detection (`go test -race ./...`)
- **Protected branches** - Warns when pushing to main/master/production
- **Quick lint** - Fast go vet check on changed files

#### Post-Checkout (Developer Reminders)

Runs after checking out a branch:

- **Dependency changes** - Reminds to run `go mod download` if go.mod changed
- **Hook installation** - Reminds to run `make setup` if hooks not installed

#### Post-Merge (Auto-Updates)

Runs after merging branches:

- **Dependency updates** - Automatically runs `go mod download` if dependencies changed
- **Build reminder** - Suggests running tests after dependency updates

**Configuration:** All hooks are defined in `lefthook.yml` in the project root.

**Performance:** Pre-commit hooks use parallel execution (~4-6s), pre-push includes full test suite (~10-30s).

These hooks mirror the GitHub CI pipeline, ensuring consistency between local development and CI/CD.

### Building

```bash
# Show all available commands
make help

# Build the binary
make build

# Build cross-platform binaries
make packages

# Build Docker image
make docker-build

# Build and run Docker container
make docker-run
```

### Testing

```bash
# Run all tests
make test

# Run tests with race detection  
make test-race

# Run benchmark tests
make test-benchmark

# Generate coverage report
make test-coverage

# Generate HTML coverage report (opens in browser)
make test-coverage-html

# Continuously run tests (requires 'watch' command)
make test-watch
```

Current test coverage: **60.1%** (target: 60%+ maintained)

### Code Quality

The project maintains high code quality through:

- **Comprehensive linting**: 45+ golangci-lint rules enabled
- **Security scanning**: gosec integration for vulnerability detection
- **Test coverage**: Maintained above 60%
- **Pre-commit hooks**: Automatic quality enforcement

### Code Quality & Linting

All code must pass the comprehensive quality checks:

```bash
# Show all available commands
make help

# Complete development workflow
make dev-setup      # Set up development environment
make dev-check      # Run all development checks  
make precommit      # Pre-commit validation

# Individual quality checks
make fmt            # Format Go code
make fmt-check      # Check if code is formatted
make vet            # Run go vet
make lint           # Run golangci-lint  
make lint-fix       # Run golangci-lint with auto-fix
make lint-full      # Complete linting suite (matches CI)
make gci-fix        # Fix import grouping/formatting
make security-check # Run gosec security analysis
make tidy           # Tidy Go modules
```

### Contributing

1. Create a feature branch: `git checkout -b feature/your-feature`
2. Make your changes following existing code patterns
3. Ensure tests pass: `make test`
4. Ensure linters pass: `make lint` (or rely on pre-commit hook)
5. Create a pull request

**Quality Enforcement:**
- The pre-commit hook automatically prevents commits that don't meet quality standards
- `.envrc` (direnv) enforces proper development environment setup
- CI/CD pipeline mirrors local linting for consistency

**Development Workflow Tips:**
- Use `gci-fix` alias to fix import formatting quickly
- Use `lint-fix` alias to auto-fix many linting issues
- Use `test-coverage` alias to generate HTML coverage reports
- The `.envrc` provides immediate feedback on environment issues

### Architecture

- **Core**: Job execution engine and scheduling
- **CLI**: Configuration management and command interface  
- **Web**: HTTP API and web interface
- **Config**: Input validation and sanitization
- **Metrics**: Prometheus monitoring integration
- **Logging**: Structured logging with context

See [Project Index](PROJECT_INDEX.md) for detailed architecture documentation.