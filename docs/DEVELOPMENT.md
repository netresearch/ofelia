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

**Option A: Native Git Hooks (Recommended)**
```bash
./scripts/install-hooks.sh
```

**Option B: Husky (for teams preferring Node.js toolchain)**
```bash
./scripts/install-husky.sh
```

Both options provide identical functionality with **parallel execution** (~5-8s vs ~15-20s sequential), automatically running all linters before each commit to ensure code quality and consistency with the CI pipeline.

### Git Hooks

The project uses Git pre-commit hooks to enforce code quality standards. The hook runs:

- **go vet** - Static analysis for common Go programming errors
- **gofmt** - Go code formatting validation
- **golangci-lint** - Comprehensive linting with 45+ enabled rules
- **gosec** - Security vulnerability scanning
- **Secret detection** - Basic check for hardcoded credentials

These are the same linters used in the GitHub CI pipeline, ensuring consistency between local development and CI/CD.

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