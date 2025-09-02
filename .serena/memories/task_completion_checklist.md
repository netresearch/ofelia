# Task Completion Checklist for Ofelia

## Before Marking a Task Complete

### 1. Code Quality Checks
```bash
# Format the code
make fmt

# Run go vet
make vet

# Run the linter
make lint

# Ensure go.mod is tidy
make tidy
```

### 2. Testing
```bash
# Run all tests
make test

# Verify CI checks pass
make ci
```

### 3. Build Verification
```bash
# Build the binary to ensure compilation
go build -o bin/ofelia ofelia.go
```

### 4. Documentation
- Update relevant documentation if API changes
- Add/update code comments for complex logic
- Update README.md if user-facing changes

### 5. Git Hygiene
- Ensure changes are in a feature branch (not main/master)
- Write clear, descriptive commit messages
- Review changes with `git diff` before committing

## Automated CI Checks
The GitHub Actions CI will automatically:
- Verify go.mod is tidy
- Check code formatting
- Run go vet
- Execute all tests
- Run golangci-lint
- Perform security scanning (gosec, govulncheck)
- Build for multiple platforms

## Critical Rules
- Never commit directly to main/master
- All tests must pass
- Code must be formatted
- Linter warnings should be addressed
- No new TODOs without corresponding GitHub issues