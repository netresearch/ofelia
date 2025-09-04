# Development Guide

## Getting Started

### Prerequisites

- Go 1.25+
- Docker and Docker Compose
- Git

### Development Setup

1. Clone the repository:
```bash
git clone https://github.com/netresearch/ofelia.git
cd ofelia
```

2. Install Git hooks for linting enforcement:
```bash
./scripts/install-hooks.sh
```

This installs a pre-commit hook that automatically runs all linters before each commit, ensuring code quality and consistency with the CI pipeline.

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
# Build the binary
make build

# Build Docker image
make docker-build

# Run tests
make test

# Run all linters (matches CI pipeline)
make lint
```

### Testing

```bash
# Run all tests
go test ./...

# Run tests with race detection
go test -race ./...

# Run tests with coverage
go test -cover ./...

# View detailed coverage report
go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out
```

Current test coverage: **60.1%** (target: 60%+ maintained)

### Code Quality

The project maintains high code quality through:

- **Comprehensive linting**: 45+ golangci-lint rules enabled
- **Security scanning**: gosec integration for vulnerability detection
- **Test coverage**: Maintained above 60%
- **Pre-commit hooks**: Automatic quality enforcement

### Linting

All code must pass the full linting suite:

```bash
# Run the complete linting suite (same as CI)
make lint

# Individual linters
go vet ./...
gofmt -l $(find . -name '*.go')
golangci-lint run --timeout=5m
gosec ./...
```

### Contributing

1. Create a feature branch: `git checkout -b feature/your-feature`
2. Make your changes following existing code patterns
3. Ensure tests pass: `make test`
4. Ensure linters pass: `make lint` (or rely on pre-commit hook)
5. Create a pull request

The pre-commit hook will automatically prevent commits that don't meet quality standards.

### Architecture

- **Core**: Job execution engine and scheduling
- **CLI**: Configuration management and command interface  
- **Web**: HTTP API and web interface
- **Config**: Input validation and sanitization
- **Metrics**: Prometheus monitoring integration
- **Logging**: Structured logging with context

See [Project Index](PROJECT_INDEX.md) for detailed architecture documentation.