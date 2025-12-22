# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.17.0] - 2025-12-22

### Added

- **Secure Web Authentication** ([#408](https://github.com/netresearch/ofelia/pull/408))
  - Complete bcrypt password hashing with HMAC session tokens
  - Secure cookie handling with HttpOnly, Secure, and SameSite flags
  - Support for reverse proxy HTTPS detection (X-Forwarded-Proto)
  - Password hashing utility: `ofelia hashpw`

- **Doctor Command Enhancements** ([#408](https://github.com/netresearch/ofelia/pull/408))
  - Web authentication configuration checks in `ofelia doctor`
  - Validates password hash format and token secret strength

- **ntfy-token Preset** ([#409](https://github.com/netresearch/ofelia/pull/409))
  - Bearer token authentication for self-hosted ntfy instances
  - Supports both ntfy.sh and self-hosted deployments with access tokens

- **Webhook Host Whitelist** ([#410](https://github.com/netresearch/ofelia/pull/410))
  - New `webhook-allowed-hosts` configuration option
  - Default: `*` (allow all hosts) - consistent with local command trust model
  - Whitelist mode when specific hosts are configured
  - Supports domain wildcards (e.g., `*.slack.com`)

- **CronClock Interface** ([#412](https://github.com/netresearch/ofelia/pull/412))
  - Testable time abstraction for scheduler testing
  - FakeClock implementation for instant, deterministic tests
  - go-cron compatible Timer interface

### Security

- **Cookie Security Hardening** ([#411](https://github.com/netresearch/ofelia/pull/411))
  - Secure, HttpOnly, and SameSite=Lax flags on all cookies
  - HTTPS detection for reverse proxy deployments
  - Security boundaries ADR documenting responsibility model

- **GitHub Actions Pinning** ([#411](https://github.com/netresearch/ofelia/pull/411))
  - All workflow actions pinned to SHA for supply chain security
  - CodeQL updated to v3.31.9

### Improved

- **Test Infrastructure** ([#412](https://github.com/netresearch/ofelia/pull/412))
  - Complete gocheck to stdlib+testify migration
  - Eventually pattern replacing time.Sleep-based synchronization
  - Parallel test execution with t.Parallel()
  - Race condition fixes detected by -race flag

- **Performance** ([#412](https://github.com/netresearch/ofelia/pull/412))
  - Sub-second scheduling for faster test execution
  - Optimized pre-commit and pre-push hooks
  - Test suite runtime reduced by ~80%

- **Linting** ([#413](https://github.com/netresearch/ofelia/pull/413))
  - Comprehensive golangci-lint configuration audit
  - All linting issues resolved

### Documentation

- **Security Boundaries ADR** ([#411](https://github.com/netresearch/ofelia/pull/411))
  - ADR-002 documenting security responsibility model
  - Clear separation between Ofelia and infrastructure responsibilities

- **Webhook Documentation** ([#410](https://github.com/netresearch/ofelia/pull/410))
  - Host whitelist configuration guide
  - Security model explanation

## [0.16.0] - 2025-12-10

### Fixed

- **Docker Socket HTTP/2 Compatibility**
  - Fixed Docker client connection failures on non-TLS connections introduced in v0.11.0
  - OptimizedDockerClient now only enables HTTP/2 for HTTPS (TLS) connections
  - HTTP/2 is disabled for Unix sockets, tcp://, and http:// (Docker daemon only supports HTTP/2 over TLS with ALPN)
  - Resolves "protocol error" issues when connecting to `/var/run/docker.sock` or `tcp://localhost:2375`
  - HTTP/2 enabled only for `https://` connections where Docker daemon supports ALPN negotiation
  - Added comprehensive unit tests covering all connection types (9 scenarios)
  - Technical details: Docker daemon does not implement h2c (HTTP/2 cleartext) - HTTP/2 requires TLS

## [0.11.0] - 2025-11-21

### Critical Fixes

- **Command Parsing in Swarm Services** ([#254](https://github.com/netresearch/ofelia/pull/254))
  - Fixed critical bug where `strings.Split` broke quoted arguments in Docker Swarm service commands
  - Now uses `args.GetArgs()` to properly handle commands like `sh -c "echo hello world"`
  - Prevents command execution failures in complex shell commands

- **LocalJob Empty Command Panic** ([#254](https://github.com/netresearch/ofelia/pull/254))
  - Fixed documented bug where empty commands caused runtime panic
  - Now returns proper error instead of crashing
  - Prevents service crashes from malformed job configurations

### Security

- **API Security Validation** ([#254](https://github.com/netresearch/ofelia/pull/254))
  - Added validation for LocalJob and ComposeJob API endpoints
  - Prevents command injection attacks via API
  - Validates file paths, service names, and command arguments

- **Privilege Escalation Logging** ([#244](https://github.com/netresearch/ofelia/pull/244))
  - Enhanced logging for security monitoring
  - Better detection of privilege escalation attempts

- **Dependency Updates**
  - Updated golang.org/x/crypto to v0.45.0 for CVE fixes

### Performance

- **Enhanced Buffer Pool** ([#245](https://github.com/netresearch/ofelia/pull/245))
  - Multi-tier adaptive pooling system
  - 99.97% memory usage reduction (2000 MB → 0.5 MB for 100 executions)
  - Automatic size adjustment and pool warmup

- **Optimized Docker Client** ([#245](https://github.com/netresearch/ofelia/pull/245))
  - Connection pooling for reduced overhead
  - Thread-safe concurrent operations
  - Health monitoring and automatic recovery

- **Reduced Polling** ([#254](https://github.com/netresearch/ofelia/pull/254))
  - Increased legacy polling interval from 500ms to 2s
  - 75% reduction in Docker API calls (200/min → 50/min per job)
  - Significant CPU and network usage improvement

- **Performance Metrics Framework** ([#245](https://github.com/netresearch/ofelia/pull/245))
  - Comprehensive metrics for Docker operations
  - Memory, latency, and throughput tracking
  - Real-time performance monitoring

### Added

- **Container Annotations**
  - Support for custom annotations on RunJob and RunServiceJob
  - Default Ofelia annotations for job tracking
  - User-defined metadata for containers and services

- **WorkingDir for ExecJob**
  - Support for setting working directory in exec jobs
  - Backward compatible with existing configurations

- **Opt-in Validation**
  - New `enable-strict-validation` flag
  - Allows gradual migration to strict validation
  - Prevents breaking changes for existing users

- **Git Hooks with Lefthook**
  - Go-native git hooks for better portability
  - Pre-commit, commit-msg, pre-push, post-checkout, post-merge hooks
  - Automated code quality checks and security scans

### Documentation

- **Architecture Diagrams** ([#252](https://github.com/netresearch/ofelia/pull/252))
  - System architecture overview
  - Component interaction diagrams
  - Data flow visualization

- **Complete Package Documentation** ([#247](https://github.com/netresearch/ofelia/pull/247))
  - Comprehensive package-level documentation
  - Security guides and best practices
  - Practical usage guides

- **Docker Requirements**
  - Documented minimum Docker version requirements
  - API compatibility notes

- **Exit Code Documentation** ([#254](https://github.com/netresearch/ofelia/pull/254))
  - Clear documentation of Ofelia-specific exit codes
  - Swarm service error codes (-999, -998)

### Fixed

- **Go Version Check** ([#251](https://github.com/netresearch/ofelia/pull/251))
  - Corrected inverted logic in .envrc Go version check
  - Ensures correct Go version enforcement

### Changed

- Updated go-dockerclient to v1.12.2
- Migrated from Husky to Lefthook for git hooks
- Improved CI/CD pipeline with comprehensive security scanning

### Internal

- Removed AI assistant artifacts and outdated documentation ([#246](https://github.com/netresearch/ofelia/pull/246), [#253](https://github.com/netresearch/ofelia/pull/253))
- Enhanced test suite with comprehensive integration tests
- Improved code organization and maintainability

## [0.10.2] - 2025-11-15

Previous release.

---

## Migration Guide v0.10.x → v0.11.0

### Breaking Changes

**None** - This release is backward compatible with v0.10.x

### Recommended Actions

1. **Review API Usage**: If you create jobs via API, ensure commands are properly validated
2. **Check Swarm Commands**: Verify complex shell commands in service jobs work correctly
3. **Monitor Performance**: Observe improved memory usage and reduced API calls
4. **Enable Metrics**: Consider enabling the new metrics framework for monitoring

### New Configuration Options

```ini
# Optional: Enable strict validation (default: false)
[global]
enable-strict-validation = true

# New: Container annotations
[job-run "example"]
annotations = com.example.key=value, app.version=1.0
```

### Deprecations

**None** in this release.

---

For more information, see:
- [Documentation](https://github.com/netresearch/ofelia/tree/main/docs)
- [Security Guide](https://github.com/netresearch/ofelia/blob/main/docs/SECURITY.md)
- [Configuration Guide](https://github.com/netresearch/ofelia/blob/main/docs/CONFIGURATION.md)
