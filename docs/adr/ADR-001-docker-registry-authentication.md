# ADR-001: Docker Registry Authentication via config.json

## Status

**Accepted** - 2025-12-10

## Context

Ofelia is a Docker job scheduler that can pull images from registries before running containers. Issue #370 (upstream #91) requests that Ofelia read Docker's `config.json` file to authenticate with private registries when pulling images.

### Current State

The codebase has infrastructure for registry authentication that is **not being used**:

1. `domain.PullOptions` has a `RegistryAuth` field (unused)
2. `domain.AuthConfig` defines the authentication structure
3. `ports.AuthProvider` interface exists but has no implementation
4. `docker.EncodeAuthConfig()` can encode auth for API calls

In `SDKDockerProvider.PullImage()`, `PullOptions` is created without setting `RegistryAuth`:

```go
opts := domain.PullOptions{
    Repository: ref.Repository,
    Tag:        ref.Tag,
    // RegistryAuth is never set!
}
```

### Problem

When Ofelia runs as a long-running daemon, it cannot:
- Authenticate with private Docker registries
- Use credential helpers (docker-credential-*)
- Detect credential updates without restart

This limits Ofelia's usability in enterprise environments where private registries are standard.

## Decision

Implement Docker registry authentication by:

1. **Adding `github.com/docker/cli/cli/config` dependency** - The canonical library for reading Docker's config.json and invoking credential helpers

2. **Implementing `ports.AuthProvider` interface** - Create `DockerConfigAuthProvider` that:
   - Reads Docker config fresh on each call (no caching)
   - Supports credential helpers (docker-credential-*)
   - Extracts registry hostname from image reference

3. **Injecting AuthProvider into SDKDockerProvider** - Modify:
   - `SDKDockerProviderConfig` to accept optional `AuthProvider`
   - `PullImage()` to call `AuthProvider.GetEncodedAuth(registry)` before pulling

4. **Wiring in CLI daemon startup** - Create the `DockerConfigAuthProvider` during initialization

### Why Fresh Reads (No Caching)

Both analysis models (9/10 confidence each) agreed that reading config fresh on each pull is preferred:

- **Avoids stale credentials** - Critical for short-lived tokens (AWS ECR, GCR)
- **Simpler implementation** - No cache invalidation complexity
- **Negligible overhead** - Config read is fast compared to network image pull
- **Security** - Always uses current credentials, respects user logouts

## Consequences

### Positive

- **Unlocks private registry support** - Essential for enterprise/production use
- **Industry standard approach** - Same pattern used by Kubernetes, Tekton, GitLab
- **Leverages existing architecture** - Uses pre-designed `AuthProvider` interface
- **Low maintenance** - Official library handles config format changes
- **Security best practice** - Uses Docker's credential helper system

### Negative

- **New dependency** - Adds `github.com/docker/cli` to go.mod
- **Cross-platform complexity** - Credential helper execution may vary by OS
- **Error handling** - Must gracefully handle missing config/credentials

### Risks

| Risk | Mitigation |
|------|------------|
| Credential helper failures | Graceful fallback to no-auth pull attempt |
| Missing config.json | Silent fallback, log at debug level |
| Docker CLI version drift | Pin dependency, monitor releases |

## Alternatives Considered

### 1. Ofelia-specific credential configuration

**Rejected** - Would create separate source of truth, bypass secure credential helpers, require storing secrets in config files.

### 2. Environment variable (`DOCKER_AUTH_CONFIG`)

**Rejected** - Less comprehensive than config.json, doesn't support credential helpers.

### 3. Caching credentials at startup

**Rejected** - Causes stale credential issues, especially with short-lived tokens (ECR, GCR).

## Implementation Plan

See `docs/adr/ADR-001-implementation-plan.md` for detailed implementation steps.

## References

- Issue #370: https://github.com/netresearch/ofelia/issues/370
- Upstream Issue #91: https://github.com/mcuadros/ofelia/issues/91
- Docker CLI config package: https://pkg.go.dev/github.com/docker/cli/cli/config
- Docker credential helpers: https://docs.docker.com/engine/reference/commandline/login/#credential-helpers

## Consensus

Multi-model analysis achieved consensus (gemini-2.5-pro: 9/10, gemini-2.5-flash: 9/10) that this approach is:

- Technically feasible with no blockers
- Architecturally sound using existing interfaces
- Industry standard and security best practice
- Moderate complexity with high user value
