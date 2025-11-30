#!/bin/bash

# Legacy wrapper for lefthook installation
# This script is kept for backwards compatibility
# Modern usage: Run 'make setup' instead

set -e

echo "🪝 Installing Git hooks via lefthook..."
echo ""
echo "ℹ️  This script is a wrapper around lefthook (Go-native git hooks)"
echo "   For better experience, run: make setup"
echo ""

# Check if lefthook is installed
if ! command -v lefthook >/dev/null 2>&1; then
    echo "📦 Installing lefthook..."
    # Pin to commit hash for OpenSSF Scorecard compliance (v2.0.4)
    go install github.com/evilmartians/lefthook@a92b0191f01bd54306f069c371878eeee39611f7
    echo "✅ lefthook installed"
fi

# Install hooks
lefthook install

echo ""
echo "✅ Git hooks installed successfully via lefthook!"
echo ""
echo "📝 Hooks configured in: lefthook.yml"
echo "🚀 Fast, parallel execution with Go-native performance"
echo ""
echo "The hooks will automatically run the following checks on each commit:"
echo "  • go mod tidy (dependency hygiene)"
echo "  • go vet (static analysis)"
echo "  • gofmt (code formatting)"
echo "  • golangci-lint (comprehensive linting)"
echo "  • golangci-lint extra (additional quality checks)"
echo "  • gosec (security analysis)"
echo "  • Secret detection (basic check)"
echo ""
echo "All checks run in parallel for maximum speed (~4-6s typical execution)"
echo ""
echo "🔧 To manage hooks: lefthook --help"
echo "📖 Configuration: lefthook.yml in project root"
