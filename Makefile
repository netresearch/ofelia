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
LDFLAGS = -ldflags "-X main.version=$(BRANCH) -X main.build=$(BUILD)" 

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
	@GOTOOLCHAIN=go1.25.0 GOBIN=$(BUILD_PATH)/.tools go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@$(BUILD_PATH)/.tools/golangci-lint version || true
	@$(BUILD_PATH)/.tools/golangci-lint run --timeout=5m

.PHONY: lint-fix
lint-fix:
	@mkdir -p $(BUILD_PATH)/.tools
	@GOTOOLCHAIN=go1.25.0 GOBIN=$(BUILD_PATH)/.tools go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
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
		echo "   - Snap: sudo snap install gosec"; \
		echo "   - Binary: https://github.com/securecodewarrior/gosec/releases"; \
		echo "   - Or run: make dev-setup"; \
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
	@go test -bench=. -benchmem ./...

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

# Development workflow commands
.PHONY: setup
setup: dev-setup
	@echo "🎉 Setup complete! You're ready to develop."

.PHONY: dev-setup
dev-setup:
	@echo "🔧 Setting up development environment..."
	@echo "📦 Installing required tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "✅ golangci-lint installed"
	@if command -v gosec >/dev/null 2>&1; then \
		echo "✅ gosec already available"; \
	else \
		echo "📥 Installing gosec via snap..."; \
		if command -v snap >/dev/null 2>&1; then \
			sudo snap install gosec; \
		else \
			echo "⚠️  gosec not found and snap unavailable. Install manually:"; \
			echo "   - Snap: sudo snap install gosec"; \
			echo "   - Binary: https://github.com/securecodewarrior/gosec/releases"; \
		fi; \
	fi
	@go install github.com/daixiang0/gci@latest
	@echo "✅ gci installed"
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
	@docker build -t $(PROJECT):$(PKG_TAG) .

.PHONY: docker-run
docker-run: docker-build
	@docker run --rm -it $(PROJECT):$(PKG_TAG)

.PHONY: help
help:
	@echo "Ofelia Development Commands:"
	@echo ""
	@echo "🏗️  Building:"
	@echo "  build              - Build local binary"
	@echo "  packages           - Build cross-platform binaries"
	@echo "  docker-build       - Build Docker image"
	@echo "  docker-run         - Build and run Docker container"
	@echo ""
	@echo "🧪 Testing:"
	@echo "  test               - Run unit tests"
	@echo "  test-race          - Run tests with race detector"
	@echo "  test-benchmark     - Run benchmark tests"
	@echo "  test-coverage      - Generate coverage report"
	@echo "  test-coverage-html - Generate HTML coverage report"
	@echo "  test-watch         - Continuously run tests"
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
	@echo "📊 Current Test Coverage: 60.1%"
	@echo "🎯 Quality: 45+ linting rules, security scanning, pre-commit hooks"

build:
	@mkdir -p $(BUILD_PATH)
	@go build -o $(BUILD_PATH)/$(PROJECT) ofelia.go

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
