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

# Check if go.mod is clean (no missing/unused dependencies)
echo "🧹 Checking go.mod cleanliness..."
if ! go mod tidy -diff >/dev/null 2>&1; then
    echo "❌ go.mod is not clean. Running go mod tidy shows differences:"
    go mod tidy -diff
    echo "Please run 'go mod tidy' and commit the changes."
    exit 1
fi

# Run linters in parallel for faster execution (~5-8s vs ~15-20s)
echo "⚡ Running linters in parallel..."

# Create temporary files for capturing output
vet_out=$(mktemp)
fmt_out=$(mktemp)
lint_out=$(mktemp)
extra_out=$(mktemp)
gosec_out=$(mktemp)

# Run linters in background processes
{
    echo "🔧 Running go vet..." >&2
    if ! go vet ./... 2>"$vet_out"; then
        echo "VET_FAILED" > "$vet_out.status"
    else
        echo "VET_PASSED" > "$vet_out.status"
    fi
} &
vet_pid=$!

{
    echo "📝 Checking Go formatting..." >&2
    unformatted_files=$(gofmt -l $(git ls-files '*.go') 2>"$fmt_out" || true)
    if [ ! -z "$unformatted_files" ]; then
        echo "$unformatted_files" > "$fmt_out"
        echo "FMT_FAILED" > "$fmt_out.status"
    else
        echo "FMT_PASSED" > "$fmt_out.status"
    fi
} &
fmt_pid=$!

{
    echo "🔍 Running golangci-lint..." >&2
    if ! golangci-lint run --timeout=3m 2>"$lint_out"; then
        echo "LINT_FAILED" > "$lint_out.status"
    else
        echo "LINT_PASSED" > "$lint_out.status"
    fi
} &
lint_pid=$!

{
    echo "🛡️ Running additional linters..." >&2
    extra_linters="gci,wrapcheck,unparam,revive,misspell,paralleltest,gocyclo,unused,forbidigo,errorlint"
    if ! golangci-lint run --timeout=3m --enable="$extra_linters" --disable-all 2>"$extra_out"; then
        echo "EXTRA_FAILED" > "$extra_out.status"
    else
        echo "EXTRA_PASSED" > "$extra_out.status"
    fi
} &
extra_pid=$!

{
    echo "🔒 Running gosec..." >&2
    if ! gosec ./... 2>"$gosec_out" >/dev/null; then
        echo "GOSEC_FAILED" > "$gosec_out.status"
    else
        echo "GOSEC_PASSED" > "$gosec_out.status"
    fi
} &
gosec_pid=$!

# Wait for all background processes and check results
failed_checks=""

wait $vet_pid
if [ "$(cat "$vet_out.status")" = "VET_FAILED" ]; then
    failed_checks="${failed_checks}go vet "
    echo "❌ go vet failed:"
    cat "$vet_out"
fi

wait $fmt_pid  
if [ "$(cat "$fmt_out.status")" = "FMT_FAILED" ]; then
    failed_checks="${failed_checks}gofmt "
    echo "❌ The following files are not properly formatted:"
    cat "$fmt_out"
    echo "Please run: gofmt -w $(cat "$fmt_out" | tr '\n' ' ')"
fi

wait $lint_pid
if [ "$(cat "$lint_out.status")" = "LINT_FAILED" ]; then
    failed_checks="${failed_checks}golangci-lint "
    echo "❌ golangci-lint failed:"
    cat "$lint_out"
fi

wait $extra_pid
if [ "$(cat "$extra_out.status")" = "EXTRA_FAILED" ]; then
    failed_checks="${failed_checks}extra-linters "
    echo "❌ Additional linters failed:"
    cat "$extra_out"
fi

wait $gosec_pid
if [ "$(cat "$gosec_out.status")" = "GOSEC_FAILED" ]; then
    failed_checks="${failed_checks}gosec "
    echo "❌ gosec found security issues:"
    cat "$gosec_out"
fi

# Clean up temporary files
rm -f "$vet_out" "$vet_out.status" "$fmt_out" "$fmt_out.status" \
      "$lint_out" "$lint_out.status" "$extra_out" "$extra_out.status" \
      "$gosec_out" "$gosec_out.status"

# Exit if any checks failed
if [ ! -z "$failed_checks" ]; then
    echo "❌ Failed checks: $failed_checks"
    echo "Please fix the issues above before committing."
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
echo "🚀 Performance optimized with parallel execution (~5-8s vs ~15-20s sequential)"
echo ""
echo "The hook will automatically run the following checks on each commit:"
echo "  • go mod tidy (dependency hygiene)"
echo "  • go vet (static analysis) - parallel"
echo "  • gofmt (code formatting) - parallel" 
echo "  • golangci-lint (comprehensive linting) - parallel"
echo "  • gosec (security analysis) - parallel"
echo "  • Secret detection (basic check)"
echo ""
echo "This ensures all commits conform to the project's quality standards."
echo "The same linters are used in the GitHub CI pipeline for consistency."
echo ""
echo "🤔 Why not Husky? This is a pure Go project without Node.js dependencies."
echo "   Native Git hooks avoid adding unnecessary toolchain complexity."
echo "   However, if you prefer Husky for consistency across projects,"
echo "   consider: npm init -y && npm install --save-dev husky"