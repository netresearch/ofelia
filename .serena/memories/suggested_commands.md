# Suggested Commands for Ofelia Development

## Building
```bash
# Build the binary
go build -o bin/ofelia ofelia.go

# Build for all platforms
make packages

# Clean build artifacts
make clean
```

## Testing
```bash
# Run all tests
go test -v ./...
make test

# Run tests with coverage
make test-coverage

# Run CI checks (format, vet, tests)
make ci
```

## Code Quality
```bash
# Format code
make fmt
gofmt -w $(git ls-files '*.go')

# Run go vet
make vet
go vet ./...

# Run linter (golangci-lint)
make lint

# Tidy go.mod
make tidy
go mod tidy
```

## Running
```bash
# Run daemon with config
./ofelia daemon --config=/path/to/config.ini

# Validate configuration
./ofelia validate --config=/path/to/config.ini

# Run with web UI
./ofelia daemon --enable-web --web-address=:8081

# Run with profiling
./ofelia daemon --enable-pprof --pprof-address=127.0.0.1:8080
```

## Docker
```bash
# Build Docker image
docker build -t ofelia .

# Run with Docker
docker run -v /var/run/docker.sock:/var/run/docker.sock:ro ofelia:latest
```

## Git Commands
```bash
# Check branch
git rev-parse --abbrev-ref HEAD

# Get commit SHA
git log --format='%H' -n 1 | cut -c1-10
```

## System Commands (Linux)
```bash
ls -la          # List files with details
cd              # Change directory
grep -r         # Recursive search
find . -name    # Find files by name
pwd             # Current directory
```