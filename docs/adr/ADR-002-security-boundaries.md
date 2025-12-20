# ADR-002: Security Boundary Definition

**Status**: Accepted  
**Date**: 2025-12-17  
**Authors**: Security Review Team

## Context

Ofelia is a job scheduler that executes commands in Docker containers and on the host system. During security review, questions arose about:

1. **What security controls should Ofelia enforce?**
   - Should Ofelia block privileged containers?
   - Should Ofelia validate/restrict commands executed in jobs?
   - Should Ofelia prevent host mounts or dangerous capabilities?

2. **What is security theater vs. actual security?**
   - Input "sanitization" that blocks shell operators but doesn't prevent malicious commands
   - Blocking `rm -rf` but allowing arbitrary code execution via Python/Node/etc.
   - Restricting container flags when the user has Docker socket access anyway

3. **Who is the actual threat actor?**
   - External attacker trying to exploit Ofelia?
   - Authorized user creating malicious jobs?
   - Compromised container escaping to host?

## Decision

**Ofelia adopts a clear separation of security responsibilities:**

### Infrastructure Responsibility (NOT Ofelia's job)

The following are delegated entirely to the deployment infrastructure:

| Control | Owner | Rationale |
|---------|-------|-----------|
| Container privileges (`--privileged`, capabilities) | Docker daemon / orchestrator | Ofelia is a scheduler, not a policy engine |
| Host mounts and volume permissions | Infrastructure | Same access level as `docker run` |
| Network isolation and firewall rules | Network infrastructure | Beyond application scope |
| Resource limits (cgroups, ulimits) | Container runtime | Already has proper enforcement |
| Security profiles (AppArmor, SELinux, seccomp) | Host OS | Kernel-level enforcement |
| Docker socket access control | Host OS / orchestrator | Ofelia needs socket access to function |
| TLS termination | Reverse proxy | Standard deployment pattern |

**Rationale**: If a user can create jobs via Ofelia's API, they have equivalent access to `docker exec` and `docker run`. Blocking specific flags or commands in Ofelia provides no security value—the user could simply run those commands directly. The trust boundary is "who can access Ofelia's API", not "what commands Ofelia allows".

### Ofelia Application Responsibility

Ofelia is responsible for:

| Control | Implementation | Purpose |
|---------|----------------|---------|
| Authentication | JWT tokens, bcrypt passwords | Verify user identity |
| Authorization | API access control (currently single-user) | Control who can create/run jobs |
| Input format validation | Cron syntax, image name format, path format | Prevent malformed input from causing errors |
| Rate limiting | Per-IP request limits | Prevent DoS, brute force |
| Session management | Token expiry, secure cookies | Limit exposure window |
| Application stability | Memory bounds, graceful shutdown | Prevent OOM, zombie processes |
| Audit logging | Security events logged | Forensics, monitoring |

### What Input Validation Actually Does

The sanitizer in `config/sanitizer.go` performs **format validation**, not **security policy enforcement**:

| Validation | Actual Purpose | NOT a Security Control Because |
|------------|----------------|-------------------------------|
| Block shell operators (`; & |`) | Prevent job config parsing errors | User could use `bash -c "cmd1; cmd2"` |
| Block `rm -rf` | Prevent accidental destructive typos | User could use Python: `shutil.rmtree()` |
| Block path traversal (`../`) | Ensure paths are well-formed | User controls container filesystem anyway |
| Validate image format | Ensure Docker can parse image name | User chooses what image to run |

This validation catches **mistakes**, not **malice**. A malicious authorized user can bypass all of it.

### Trust Model

```
┌─────────────────────────────────────────────────────────────────┐
│                    TRUST BOUNDARY                                │
│         (Authentication: who can access Ofelia's API)            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  INSIDE (trusted):                                               │
│  • Authenticated API users can create ANY job                    │
│  • Jobs execute with container's permissions                     │
│  • INI file authors (trusted config source)                      │
│                                                                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  OUTSIDE (untrusted):                                            │
│  • Unauthenticated requests → rejected                           │
│  • Docker labels (partially trusted, LocalJob restricted)        │
│  • Job output (logged but not trusted as input)                  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Key insight**: The security question is "who can authenticate to Ofelia?", not "what can authenticated users do?". Authenticated users are trusted to create jobs.

## Consequences

### Positive

1. **Clear responsibility model** - Operators know what to secure at each layer
2. **No false sense of security** - We don't claim to block attacks we can't actually prevent
3. **Simpler codebase** - No complex command parsing trying to guess intent
4. **Works with existing infrastructure** - Integrates with Docker's native security model
5. **Accurate documentation** - SECURITY.md reflects actual guarantees

### Negative

1. **Requires secure deployment** - Operators must configure Docker/K8s security properly
2. **Single-user model limits multi-tenant use** - No per-user permissions yet
3. **JWT tokens not revocable** - Must use short expiry or restart to invalidate

### Neutral

1. Input validation remains for **format correctness**, not security
2. LocalJob restrictions from Docker labels remain (defense-in-depth for that specific vector)
3. Future RBAC could add per-user command restrictions if needed

## Deployment Implications

### For Self-Hosted Docker

```bash
# Security is at Docker level, not Ofelia level
docker run \
  --security-opt=no-new-privileges \
  --cap-drop=ALL \
  --read-only \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/netresearch/ofelia:latest
```

### For Kubernetes

```yaml
# Use Pod Security Standards (PSS), not Ofelia config
# Apply to namespace via labels (K8s 1.23+)
apiVersion: v1
kind: Namespace
metadata:
  name: ofelia
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/warn: restricted
```

### For Multi-Tenant Environments

Current single-user auth is insufficient. Options:
1. Run separate Ofelia instance per tenant
2. Put Ofelia behind auth proxy with tenant isolation
3. Wait for future RBAC implementation

## References

- [SECURITY.md - Security Responsibility Model](../SECURITY.md#security-responsibility-model)
- [config/sanitizer.go](../../config/sanitizer.go) - Input format validation
- [web/auth_secure.go](../../web/auth_secure.go) - Authentication implementation
- Docker Security Best Practices: https://docs.docker.com/engine/security/
