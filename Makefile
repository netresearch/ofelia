# Package configuration
PROJECT = ofelia
COMMANDS = ofelia

# Environment
BASE_PATH := $(shell pwd)
BUILD_PATH := $(BASE_PATH)/bin
SHA1 := $(shell git log --format='%H' -n 1 | cut -c1-10)
BUILD := $(shell date +"%m-%d-%Y_%H_%M_%S")
BRANCH := $(shell git rev-parse --abbrev-ref HEAD | sed 's/\//-/g')

# Packages content
PKG_OS = darwin linux
PKG_ARCH = amd64
PKG_CONTENT =
PKG_TAG = latest

# Go parameters
GOCMD = go
GOBUILD = $(GOCMD) build
GHRELEASE = github-release
# BUILD_FLAGS defaults to stripping debug info for smaller binaries.
# Override to preserve debug symbols: make build BUILD_FLAGS=""
# Or build with: make build-debug
BUILD_FLAGS ?= -s -w
LDFLAGS = -ldflags "$(BUILD_FLAGS) -X main.version=$(BRANCH) -X main.build=$(BUILD)"

# Coverage
COVERAGE_REPORT = coverage.txt
COVERAGE_MODE = atomic

ifneq ($(origin TRAVIS_TAG), undefined)
	BRANCH := $(TRAVIS_TAG)
endif

# Default rule shows help
.DEFAULT_GOAL := help

# Rules  
all: clean packages

.PHONY: fmt
fmt:
	@gofmt -w $$(git ls-files '*.go')

.PHONY: vet
vet:
	@go vet ./...

.PHONY: tidy
tidy:
	@go mod tidy

.PHONY: lint
lint:
	@mkdir -p $(BUILD_PATH)/.tools
	@GOTOOLCHAIN=go1.26.4 GOBIN=$(BUILD_PATH)/.tools go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@$(BUILD_PATH)/.tools/golangci-lint version || true
	@$(BUILD_PATH)/.tools/golangci-lint run --timeout=5m

.PHONY: lint-fix
lint-fix:
	@mkdir -p $(BUILD_PATH)/.tools
	@GOTOOLCHAIN=go1.26.4 GOBIN=$(BUILD_PATH)/.tools go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@$(BUILD_PATH)/.tools/golangci-lint run --fix --timeout=5m

.PHONY: lint-full
lint-full: vet fmt-check lint security-check
	@echo "✅ All linting checks passed!"

.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l $$(git ls-files '*.go')); \
	if [ -n "$$unformatted" ]; then \
	  echo "❌ The following files are not formatted:" >&2; \
	  echo "$$unformatted" >&2; \
	  echo "Run: make fmt" >&2; \
	  exit 1; \
	fi
	@echo "✅ All Go files are properly formatted"

.PHONY: gci-fix
gci-fix:
	@if command -v gci >/dev/null 2>&1; then \
		gci write --skip-generated -s standard -s default -s "prefix(github.com/netresearch/ofelia)" .; \
		echo "✅ Import grouping fixed with gci"; \
	else \
		echo "❌ gci not found. Install with: go install github.com/daixiang0/gci@latest"; \
		exit 1; \
	fi

.PHONY: security-check
security-check:
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
		echo "✅ Security check passed"; \
	else \
		echo "❌ gosec not found. Install with:"; \
		echo "   go install github.com/securego/gosec/v2/cmd/gosec@latest"; \
		echo "   Or run: make dev-setup"; \
		exit 1; \
	fi

.PHONY: ci
ci: vet
	@unformatted=$$(gofmt -l $$(git ls-files '*.go')); \
	if [ -n "$$unformatted" ]; then \
	  echo "The following files are not formatted:" >&2; \
	  echo "$$unformatted" >&2; \
	  exit 1; \
	fi
	@go test ./...

.PHONY: test
test: 
	@go test -v ./...

.PHONY: test-coverage
test-coverage: 
	@echo "mode: $(COVERAGE_MODE)" > $(COVERAGE_REPORT);
	@go test -v ./... $${p} -coverprofile=tmp_$(COVERAGE_REPORT) -covermode=$(COVERAGE_MODE); 
	cat tmp_$(COVERAGE_REPORT) | grep -v "mode: $(COVERAGE_MODE)" >> $(COVERAGE_REPORT); 
	rm tmp_$(COVERAGE_REPORT);

.PHONY: test-coverage-html
test-coverage-html: test-coverage
	@go tool cover -html=$(COVERAGE_REPORT) -o coverage.html
	@echo "✅ Coverage report generated: coverage.html"
	@echo "📊 Open coverage.html in your browser to view detailed coverage"

.PHONY: test-race
test-race:
	@go test -race -v ./...

.PHONY: test-benchmark
test-benchmark:
	@go test -run=^$$ -bench=. -benchmem ./...

.PHONY: test-watch
test-watch:
	@if command -v watch >/dev/null 2>&1; then \
		watch -n 2 "go test -v ./..."; \
	else \
		echo "❌ watch command not found. Install with your package manager"; \
		echo "  Ubuntu/Debian: sudo apt install watch"; \
		echo "  macOS: brew install watch"; \
		exit 1; \
	fi

.PHONY: test-fast
test-fast:
	@echo "⚡ Running fast unit tests (pre-commit)..."
	@go test -short -timeout=30s ./config/... ./logging/... ./metrics/...
	@echo "✅ Fast tests passed"

.PHONY: test-smoke
test-smoke:
	@echo "🚀 Running smoke tests (fast feedback on core packages)..."
	@go test -short -race -timeout=60s ./core/... ./cli/... ./middlewares/...
	@echo "✅ Smoke tests passed"

.PHONY: test-integration
test-integration:
	@echo "🐳 Running integration tests (requires Docker daemon)..."
	@go test -tags=integration -v ./...

# Mutation testing commands
.PHONY: mutation-test
mutation-test:
	@if command -v gremlins >/dev/null 2>&1; then \
		echo "🧬 Running mutation tests..."; \
		gremlins unleash --config=.gremlins.yaml; \
	else \
		echo "❌ gremlins not found. Install with:"; \
		echo "   go install github.com/go-gremlins/gremlins/cmd/gremlins@latest"; \
		exit 1; \
	fi

.PHONY: mutation-test-diff
mutation-test-diff:
	@if command -v gremlins >/dev/null 2>&1; then \
		echo "🧬 Running mutation tests on changed files..."; \
		gremlins unleash --config=.gremlins.yaml --diff; \
	else \
		echo "❌ gremlins not found. Install with:"; \
		echo "   go install github.com/go-gremlins/gremlins/cmd/gremlins@latest"; \
		exit 1; \
	fi

.PHONY: mutation-test-docker
mutation-test-docker:
	@if command -v gremlins >/dev/null 2>&1; then \
		echo "🧬🐳 Running Docker adapter mutation tests with integration tests..."; \
		echo "⏱️  This takes ~10 minutes (requires Docker daemon)"; \
		gremlins unleash ./core/adapters/docker --config=.gremlins-docker.yaml --tags integration; \
	else \
		echo "❌ gremlins not found. Install with:"; \
		echo "   go install github.com/go-gremlins/gremlins/cmd/gremlins@latest"; \
		exit 1; \
	fi

# Development workflow commands
.PHONY: setup
setup: dev-setup
	@echo "🎉 Setup complete! You're ready to develop."

.PHONY: dev-setup
dev-setup:
	@echo "🔧 Setting up development environment..."
	@echo "📦 Installing required tools..."
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@echo "✅ golangci-lint installed"
	@if command -v gosec >/dev/null 2>&1; then \
		echo "✅ gosec already available"; \
	else \
		echo "📥 Installing gosec..."; \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
		echo "✅ gosec installed"; \
	fi
	@go install github.com/daixiang0/gci@latest
	@echo "✅ gci installed"
	@go install github.com/go-gremlins/gremlins/cmd/gremlins@latest
	@echo "✅ gremlins installed (mutation testing)"
	@echo "🪝 Installing lefthook (Go-native git hooks)..."
	@go install github.com/evilmartians/lefthook@latest
	@lefthook install
	@echo "✅ Git hooks installed via lefthook"
	@echo "✅ Development environment setup complete!"

.PHONY: dev-check
dev-check: fmt-check vet lint security-check test
	@echo "🎉 All development checks passed! Ready to commit."

.PHONY: precommit
precommit: dev-check
	@echo "✅ Pre-commit checks complete - your code is ready!"

.PHONY: docker-build
docker-build:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=$$(go env GOARCH) go build -trimpath \
		-ldflags="-s -w -X main.version=dev -X main.build=$$(git rev-parse --short HEAD)" \
		-o bin/ofelia-linux-$$(go env GOARCH) .
	docker build -t $(PROJECT):$(PKG_TAG) .

.PHONY: docker-run
docker-run: docker-build
	@docker run --rm -it $(PROJECT):$(PKG_TAG)

.PHONY: help
help:
	@echo "Ofelia Development Commands:"
	@echo ""
	@echo "🏗️  Building:"
	@echo "  build              - Build local binary (stripped, no debug symbols)"
	@echo "  build-debug        - Build local binary with debug symbols preserved"
	@echo "  packages           - Build cross-platform binaries"
	@echo "  docker-build       - Build Docker image"
	@echo "  docker-run         - Build and run Docker container"
	@echo ""
	@echo "🧪 Testing:"
	@echo "  test               - Run unit tests"
	@echo "  test-smoke         - Run smoke tests (fast feedback on core packages)"
	@echo "  test-integration   - Run integration tests (requires Docker)"
	@echo "  test-race          - Run tests with race detector"
	@echo "  test-benchmark     - Run benchmark tests"
	@echo "  test-coverage      - Generate coverage report"
	@echo "  test-coverage-html - Generate HTML coverage report"
	@echo "  test-watch         - Continuously run tests"
	@echo ""
	@echo "🧬 Mutation Testing:"
	@echo "  mutation-test        - Run full mutation tests"
	@echo "  mutation-test-diff   - Run mutation tests on changed files only"
	@echo "  mutation-test-docker - Run Docker adapter mutation tests with integration"
	@echo ""
	@echo "🔍 Code Quality:"
	@echo "  fmt                - Format Go code"
	@echo "  fmt-check          - Check if code is formatted"
	@echo "  vet                - Run go vet"
	@echo "  lint               - Run golangci-lint"
	@echo "  lint-fix           - Run golangci-lint with auto-fix"
	@echo "  lint-full          - Run complete linting suite"
	@echo "  gci-fix            - Fix import grouping"
	@echo "  security-check     - Run gosec security analysis"
	@echo ""
	@echo "🚀 Development Workflow:"
	@echo "  setup              - Set up development environment (alias for dev-setup)"
	@echo "  dev-setup          - Set up development environment with lefthook"
	@echo "  dev-check          - Run all development checks"
	@echo "  precommit          - Run pre-commit validation"
	@echo "  ci                 - Run CI checks locally"
	@echo "  tidy               - Tidy Go modules"
	@echo ""
	@echo "📊 Test Coverage: run 'make test-coverage' for current numbers"
	@echo "🎯 Quality: 45+ linting rules, security scanning, pre-commit hooks"

build:
	@mkdir -p $(BUILD_PATH)
	@go build $(LDFLAGS) -o $(BUILD_PATH)/$(PROJECT) ofelia.go

.PHONY: build-debug
build-debug: BUILD_FLAGS =
build-debug: build
	@echo "Built with debug symbols preserved (no -s -w flags)"

packages:
	@for os in $(PKG_OS); do \
		for arch in $(PKG_ARCH); do \
			cd $(BASE_PATH); \
			FINAL_PATH=$(BUILD_PATH)/$(PROJECT)_$${os}_$${arch}; \
			mkdir -p $${FINAL_PATH}; \
			for cmd in $(COMMANDS); do \
				BINARY=$(BUILD_PATH)/$(PROJECT)_$${os}_$${arch}/$${cmd};\
				GOOS=$${os} GOARCH=$${arch} $(GOCMD) build -ldflags "-X main.version=$(BRANCH) -X main.build=$(BUILD)" -o $${BINARY} $${cmd}.go;\
				du -h $${BINARY};\
			done; \
			for content in $(PKG_CONTENT); do \
				cp -rfv $${content} $(BUILD_PATH)/$(PROJECT)_$${os}_$${arch}/; \
			done; \
			TAR_PATH=$(BUILD_PATH)/$(PROJECT)_$(BRANCH)_$${os}_$${arch}.tar.gz;\
			cd  $(BUILD_PATH) && tar -cvzf $${TAR_PATH} $(PROJECT)_$${os}_$${arch}/; \
			du -h $${TAR_PATH};\
		done; \
	done;

clean:
	@rm -rf $(BUILD_PATH)
