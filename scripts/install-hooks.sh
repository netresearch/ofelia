#!/bin/bash

# Install Git hooks for the Ofelia project
# This script sets up pre-commit hooks that enforce linting standards

set -e

echo "🔧 Installing Git hooks for Ofelia project..."

# Create hooks directory if it doesn't exist
mkdir -p .git/hooks

# Create pre-commit hook
cat > .git/hooks/pre-commit << 'EOF'
#!/bin/bash

# Pre-commit hook to run all linters and ensure code quality
# Based on the GitHub CI pipeline linting jobs

set -e

echo "🔍 Running pre-commit linting checks..."

# Check if this is an initial commit (no HEAD)
if git rev-parse --verify HEAD >/dev/null 2>&1; then
    against=HEAD
else
    # Initial commit: diff against an empty tree object
    against=$(git hash-object -t tree /dev/null)
fi

# Get list of staged Go files
staged_go_files=$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' || true)

if [ -z "$staged_go_files" ]; then
    echo "✅ No Go files staged, skipping Go linting"
    exit 0
fi

echo "📁 Staged Go files: $staged_go_files"

# 1. Go vet - check for common Go programming errors
echo "🔧 Running go vet..."
if ! go vet ./...; then
    echo "❌ go vet failed. Please fix the issues above."
    exit 1
fi

# 2. gofmt - check Go code formatting
echo "📝 Checking Go formatting with gofmt..."
unformatted_files=$(gofmt -l $(git ls-files '*.go') || true)
if [ ! -z "$unformatted_files" ]; then
    echo "❌ The following files are not properly formatted:"
    echo "$unformatted_files"
    echo "Please run: gofmt -w $unformatted_files"
    exit 1
fi

# 3. golangci-lint - comprehensive Go linting
echo "🔍 Running golangci-lint..."
if ! golangci-lint run --timeout=5m; then
    echo "❌ golangci-lint failed. Please fix the issues above."
    exit 1
fi

# 4. Additional linters from CI pipeline
echo "🛡️ Running additional security and quality linters..."

# Run specific extra linters that are enabled in CI
extra_linters="gci,wrapcheck,unparam,revive,misspell,paralleltest,gocyclo,unused,forbidigo,errorlint"
if ! golangci-lint run --timeout=5m --enable="$extra_linters" --disable-all; then
    echo "❌ Additional linters failed. Please fix the issues above."
    exit 1
fi

# 5. gosec - Go security checker
echo "🔒 Running gosec security analysis..."
if ! gosec ./...; then
    echo "❌ gosec found security issues. Please fix them above."
    exit 1
fi

# 6. Check for sensitive information (basic check)
echo "🕵️ Checking for sensitive information..."
if git diff --cached --name-only | xargs grep -l -E "(password|secret|key|token).*=.*['\"][^'\"]*['\"]" 2>/dev/null; then
    echo "❌ Potential sensitive information detected in staged files."
    echo "Please review and remove any hardcoded secrets before committing."
    exit 1
fi

echo "✅ All pre-commit linting checks passed!"
echo "🚀 Ready to commit."

exit 0
EOF

# Make the hook executable
chmod +x .git/hooks/pre-commit

echo "✅ Pre-commit hook installed successfully!"
echo ""
echo "The hook will automatically run the following checks on each commit:"
echo "  • go vet (static analysis)"
echo "  • gofmt (code formatting)"
echo "  • golangci-lint (comprehensive linting)"
echo "  • gosec (security analysis)"
echo "  • Secret detection (basic check)"
echo ""
echo "This ensures all commits conform to the project's quality standards."
echo "The same linters are used in the GitHub CI pipeline for consistency."