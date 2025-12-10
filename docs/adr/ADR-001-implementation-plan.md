# ADR-001: Implementation Plan - Docker Registry Authentication

## Overview

This document details the implementation steps for Docker registry authentication via config.json (ADR-001).

## Phase 1: Add Dependency

### Step 1.1: Update go.mod

```bash
go get github.com/docker/cli/cli/config
```

**Files Changed:**
- `go.mod`
- `go.sum`

## Phase 2: Implement AuthProvider

### Step 2.1: Create DockerConfigAuthProvider

**File:** `core/adapters/docker/auth.go`

```go
package docker

import (
    "github.com/docker/cli/cli/config"
    "github.com/docker/cli/cli/config/credentials"
    "github.com/docker/cli/cli/config/types"

    "github.com/netresearch/ofelia/core/domain"
)

// DockerConfigAuthProvider implements ports.AuthProvider using Docker's config.json.
// It reads credentials fresh on each call to support dynamic credential updates.
type DockerConfigAuthProvider struct {
    // configDir overrides the default Docker config directory (for testing)
    configDir string
}

// NewDockerConfigAuthProvider creates a new auth provider.
func NewDockerConfigAuthProvider() *DockerConfigAuthProvider {
    return &DockerConfigAuthProvider{}
}

// NewDockerConfigAuthProviderWithDir creates an auth provider with custom config dir.
func NewDockerConfigAuthProviderWithDir(configDir string) *DockerConfigAuthProvider {
    return &DockerConfigAuthProvider{configDir: configDir}
}

// GetAuthConfig returns auth configuration for a registry.
func (p *DockerConfigAuthProvider) GetAuthConfig(registry string) (domain.AuthConfig, error) {
    // Load config fresh each time (no caching)
    cfg, err := p.loadConfig()
    if err != nil {
        return domain.AuthConfig{}, nil // Graceful fallback
    }

    // Normalize registry address
    registry = normalizeRegistry(registry)

    // Get auth from config (supports credential helpers)
    authConfig, err := cfg.GetAuthConfig(registry)
    if err != nil {
        return domain.AuthConfig{}, nil // Graceful fallback
    }

    return convertAuthConfig(authConfig), nil
}

// GetEncodedAuth returns base64-encoded auth for a registry.
func (p *DockerConfigAuthProvider) GetEncodedAuth(registry string) (string, error) {
    auth, err := p.GetAuthConfig(registry)
    if err != nil {
        return "", err
    }

    // Empty auth is valid (public registry)
    if auth.Username == "" && auth.Password == "" && auth.IdentityToken == "" {
        return "", nil
    }

    return EncodeAuthConfig(auth)
}

func (p *DockerConfigAuthProvider) loadConfig() (*configfile.ConfigFile, error) {
    if p.configDir != "" {
        return config.Load(p.configDir)
    }
    return config.Load(config.Dir())
}

func normalizeRegistry(registry string) string {
    // Docker Hub special cases
    if registry == "" || registry == "docker.io" || registry == "index.docker.io" {
        return "https://index.docker.io/v1/"
    }
    return registry
}

func convertAuthConfig(src types.AuthConfig) domain.AuthConfig {
    return domain.AuthConfig{
        Username:      src.Username,
        Password:      src.Password,
        Auth:          src.Auth,
        Email:         src.Email,
        ServerAddress: src.ServerAddress,
        IdentityToken: src.IdentityToken,
        RegistryToken: src.RegistryToken,
    }
}
```

### Step 2.2: Add Helper to Extract Registry from Image

**File:** `core/adapters/docker/auth.go` (append)

```go
// ExtractRegistry extracts the registry hostname from an image reference.
func ExtractRegistry(image string) string {
    ref := domain.ParseRepositoryTag(image)
    repo := ref.Repository

    // Check for registry prefix (contains . or :)
    if idx := strings.Index(repo, "/"); idx > 0 {
        prefix := repo[:idx]
        if strings.Contains(prefix, ".") || strings.Contains(prefix, ":") {
            return prefix
        }
    }

    // Default to Docker Hub
    return "docker.io"
}
```

## Phase 3: Integrate into SDKDockerProvider

### Step 3.1: Update SDKDockerProviderConfig

**File:** `core/docker_sdk_provider.go`

```go
// SDKDockerProviderConfig configures the SDK provider.
type SDKDockerProviderConfig struct {
    Host            string
    Logger          Logger
    MetricsRecorder MetricsRecorder
    AuthProvider    ports.AuthProvider // NEW: Optional auth provider
}
```

### Step 3.2: Store AuthProvider in Provider

```go
type SDKDockerProvider struct {
    client          ports.DockerClient
    logger          Logger
    metricsRecorder MetricsRecorder
    authProvider    ports.AuthProvider // NEW
}
```

### Step 3.3: Update NewSDKDockerProvider

```go
func NewSDKDockerProvider(cfg *SDKDockerProviderConfig) (*SDKDockerProvider, error) {
    // ... existing code ...

    var authProvider ports.AuthProvider
    if cfg != nil {
        logger = cfg.Logger
        metricsRecorder = cfg.MetricsRecorder
        authProvider = cfg.AuthProvider // NEW
    }

    return &SDKDockerProvider{
        client:          client,
        logger:          logger,
        metricsRecorder: metricsRecorder,
        authProvider:    authProvider, // NEW
    }, nil
}
```

### Step 3.4: Update PullImage to Use Auth

```go
func (p *SDKDockerProvider) PullImage(ctx context.Context, image string) error {
    p.recordOperation("pull_image")

    ref := domain.ParseRepositoryTag(image)
    opts := domain.PullOptions{
        Repository: ref.Repository,
        Tag:        ref.Tag,
    }

    // NEW: Get registry auth if provider configured
    if p.authProvider != nil {
        registry := dockeradapter.ExtractRegistry(image)
        if auth, err := p.authProvider.GetEncodedAuth(registry); err == nil && auth != "" {
            opts.RegistryAuth = auth
            p.logDebug("Using registry auth for %s", registry)
        }
    }

    if err := p.client.Images().PullAndWait(ctx, opts); err != nil {
        p.recordError("pull_image")
        return WrapImageError("pull", image, err)
    }

    p.logNotice("Pulled image %s", image)
    return nil
}
```

## Phase 4: Wire Up in CLI

### Step 4.1: Create AuthProvider in Daemon

**File:** `cli/daemon.go`

In `buildProvider()` or equivalent initialization:

```go
import dockeradapter "github.com/netresearch/ofelia/core/adapters/docker"

func (c *DaemonCommand) createDockerProvider() (*core.SDKDockerProvider, error) {
    authProvider := dockeradapter.NewDockerConfigAuthProvider()

    return core.NewSDKDockerProvider(&core.SDKDockerProviderConfig{
        Logger:       c.Logger,
        AuthProvider: authProvider,
    })
}
```

## Phase 5: Testing

### Step 5.1: Unit Tests for AuthProvider

**File:** `core/adapters/docker/auth_test.go`

```go
func TestDockerConfigAuthProvider_GetAuthConfig(t *testing.T) {
    // Test with mock config directory
}

func TestDockerConfigAuthProvider_GetEncodedAuth(t *testing.T) {
    // Test encoding
}

func TestExtractRegistry(t *testing.T) {
    tests := []struct {
        image    string
        expected string
    }{
        {"alpine", "docker.io"},
        {"nginx:latest", "docker.io"},
        {"gcr.io/project/image:tag", "gcr.io"},
        {"localhost:5000/myimage", "localhost:5000"},
        {"registry.example.com/org/image", "registry.example.com"},
        {"192.168.1.1:5000/image", "192.168.1.1:5000"},
    }
    // ...
}

func TestDockerConfigAuthProvider_GracefulFallback(t *testing.T) {
    // Test missing config, invalid credentials
}
```

### Step 5.2: Integration Tests

**File:** `core/adapters/docker/auth_integration_test.go`

```go
//go:build integration

func TestDockerConfigAuthProvider_RealConfig(t *testing.T) {
    // Only runs when real Docker config exists
}
```

## File Summary

| File | Action | Description |
|------|--------|-------------|
| `go.mod` | Modify | Add docker/cli dependency |
| `core/adapters/docker/auth.go` | Create | AuthProvider implementation |
| `core/adapters/docker/auth_test.go` | Create | Unit tests |
| `core/adapters/docker/auth_integration_test.go` | Create | Integration tests |
| `core/docker_sdk_provider.go` | Modify | Add AuthProvider support |
| `cli/daemon.go` | Modify | Wire up AuthProvider |

## Validation Checklist

- [ ] `go mod tidy` succeeds
- [ ] All existing tests pass
- [ ] New unit tests pass
- [ ] Linter passes
- [ ] Integration tests pass (if Docker available)
- [ ] Manual test with private registry (optional)

## Rollback Plan

If issues arise:
1. Set `AuthProvider: nil` in daemon.go to disable feature
2. Feature is opt-in via the provider interface, no breaking changes

## Timeline Estimate

| Phase | Effort |
|-------|--------|
| Phase 1: Dependency | 15 min |
| Phase 2: AuthProvider | 2-3 hours |
| Phase 3: Integration | 1-2 hours |
| Phase 4: CLI Wiring | 30 min |
| Phase 5: Testing | 2-3 hours |
| **Total** | **6-9 hours** |
