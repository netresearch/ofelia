# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| < latest | :x:               |

We recommend always running the latest version to benefit from security updates.

## Reporting a Vulnerability

We take security seriously. If you discover a security vulnerability, please report it responsibly.

### How to Report

1. **Do NOT open a public GitHub issue** for security vulnerabilities
2. Report via [GitHub Security Advisories](https://github.com/netresearch/ofelia/security/advisories/new)
3. Or email the maintainers directly (see repository contacts)

### What to Include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### Response Timeline

- **Initial response**: Within 48 hours
- **Status update**: Within 7 days
- **Resolution target**: Within 30 days (depending on severity)

### Disclosure Policy

- We follow coordinated disclosure
- Security fixes are released as soon as possible
- Public disclosure after patch is available

## Security Measures

This project implements several security measures:

### Supply Chain Security

- **SLSA Level 3** provenance for all release binaries
- **Signed checksums** using Sigstore/Cosign
- **SBOM generation** for all releases
- **Dependency scanning** via Dependabot and Trivy

### Code Security

- **Static analysis** via CodeQL and gosec
- **Secret scanning** via gitleaks
- **Vulnerability scanning** via govulncheck
- **License compliance** checks

### Container Security

- **Signed container images** via Cosign
- **SBOM and provenance** attestations
- **Multi-arch builds** from trusted base images

## Verifying Releases

### Verify Binary Provenance

```bash
slsa-verifier verify-artifact ofelia-linux-amd64 \
  --provenance-path ofelia-linux-amd64.intoto.jsonl \
  --source-uri github.com/netresearch/ofelia
```

### Verify Checksums Signature

```bash
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity "https://github.com/netresearch/ofelia/.github/workflows/release-slsa.yml@refs/tags/<TAG>" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
```

### Verify Container Image

```bash
cosign verify ghcr.io/netresearch/ofelia:<TAG>
```

## OpenSSF Scorecard Notes

### Signed-Releases Score (Expected: False Negative)

The OpenSSF Scorecard may report a low or zero score for "Signed-Releases" despite this project implementing **superior** supply chain security measures:

| What Scorecard Expects | What We Implement |
|------------------------|-------------------|
| GPG signatures on release assets | ✅ SLSA Level 3 provenance attestations |
| | ✅ Cosign keyless signing (Sigstore) |
| | ✅ Signed checksums with certificate chain |
| | ✅ SBOM generation for all releases |

**Why this is a false negative**: SLSA Level 3 provenance with Sigstore/Cosign provides stronger guarantees than traditional GPG signing:
- Provenance attestations prove the exact source commit, build environment, and workflow
- Keyless signing eliminates key management risks
- Transparency log (Rekor) provides public audit trail
- Certificate-based identity tied to GitHub Actions OIDC

See [Verifying Releases](#verifying-releases) for verification commands.

### Solo-Developer Workflow Limitations

Some Scorecard checks are designed for team-based development and will show lower scores for solo-maintainer projects:

- **Code-Review**: Requires external approvers (not applicable for solo-dev)
- **Branch-Protection**: Partial score due to 0-approval requirement

These are accepted trade-offs documented as part of our security model.

## Branch Protection Settings

For OpenSSF Scorecard compliance while maintaining solo-developer workflow:

### Recommended Settings (GitHub → Settings → Branches → main)

| Setting | Value | Notes |
|---------|-------|-------|
| Require pull request before merging | ✅ Enabled | Core requirement |
| Required approvals | 0 | Solo-dev compatible |
| Dismiss stale reviews | ✅ Enabled | Optional |
| Require status checks to pass | ✅ Enabled | CI must pass |
| Required status checks | `unit tests`, `lint`, `codeql` | Key checks |
| Require branches up to date | ✅ Enabled | Prevents merge conflicts |
| Restrict force pushes | ✅ Enabled | Protects history |
| Allow deletions | ❌ Disabled | Protects main branch |

### Solo Developer Workflow

With these settings, solo developers can:
1. Create feature branches: `git checkout -b feature/xyz`
2. Push changes and create PR
3. Wait for CI to pass
4. Merge without requiring external approval

### OpenSSF Scorecard Checks

This repository targets the following scorecard improvements:

- ✅ **Pinned-Dependencies**: All GitHub Actions pinned by SHA
- ✅ **Token-Permissions**: Minimal permissions in workflows
- ✅ **Security-Policy**: This file exists
- ✅ **SAST**: CodeQL and gosec enabled
- ✅ **Dangerous-Workflow**: No dangerous patterns
- ⚠️ **Branch-Protection**: Configure via GitHub UI (see above)
- ⚠️ **Code-Review**: Enabled via branch protection
