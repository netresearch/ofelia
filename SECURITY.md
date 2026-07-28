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

Releases are built by a reusable workflow in `netresearch/.github`, so the
signing identity is that workflow rather than this repository. Verification
fails without the `--certificate-identity-regexp` / `--signer-workflow` flags
below.

### Verify a Binary

Every asset ships a bundled signature next to it (`<asset>.sigstore.json`):

```bash
cosign verify-blob \
  --bundle ofelia-linux-amd64.sigstore.json \
  --certificate-identity-regexp "^https://github\.com/netresearch/\.github/\.github/workflows/release-go-app\.yml@" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  ofelia-linux-amd64
```

### Verify Checksums

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp "^https://github\.com/netresearch/\.github/\.github/workflows/release-go-app\.yml@" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

### Verify Build Provenance

```bash
gh attestation verify <artifact> \
  --repo netresearch/ofelia \
  --signer-workflow netresearch/.github/.github/workflows/release-go-app.yml
```

### Verify Container Image

The image tag has no `v` prefix, while the release tag does — release `v0.28.0`
publishes `ghcr.io/netresearch/ofelia:0.28.0`. Pasting the release tag here
fails with a manifest-not-found error rather than a signature error.

```bash
cosign verify ghcr.io/netresearch/ofelia:0.28.0 \
  --certificate-identity-regexp "^https://github\.com/netresearch/\.github/\.github/workflows/release-go-app\.yml@" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com"

gh attestation verify oci://ghcr.io/netresearch/ofelia:0.28.0 \
  --repo netresearch/ofelia \
  --signer-workflow netresearch/.github/.github/workflows/release-go-app.yml
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

### Pinned-Dependencies: First-Party Reusable Workflows

The `Pinned-Dependencies` check flags references to reusable workflows that are not pinned by commit SHA. This project intentionally references its **own organisation's** reusable workflows via the `@main` ref:

```yaml
uses: netresearch/.github/.github/workflows/<workflow>.yml@main
```

**Third-party actions** (anything outside the `netresearch/` GitHub organisation) MUST still be pinned by full commit SHA — this is enforced by code review and by the Dependabot configuration (`.github/dependabot.yml`), which auto-bumps every SHA-pinned third-party action in the `github-actions` ecosystem on a weekly schedule. The trivy-action supply-chain incident of 2026 reinforced this rule.

**Rationale for `@main` on first-party workflows:**

- The `netresearch/.github` repository is owned by the same organisation and protected by the same access controls as this repository
- Pinning by SHA on first-party reusable workflows would require a Renovate/Dependabot cycle across every consumer repository for every improvement to the shared workflow, with no security benefit (the source of truth is the same set of maintainers)
- The shared workflows themselves pin all their internal third-party action calls by SHA, so the supply-chain surface area is contained at the central repository
- Trade-off accepted: a compromise of a `netresearch/.github` maintainer account would cascade to consumers. This is mitigated by branch protection and the same review requirements on the central repository

Open Scorecard "Pinned-Dependencies" alerts that reference `netresearch/.github/.github/workflows/*.yml@main` are dismissed as "Won't fix — first-party reusable workflow, see SECURITY.md".

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

- ✅ **Pinned-Dependencies**: All third-party GitHub Actions pinned by SHA (first-party `netresearch/.github` reusable workflows use `@main` — see [policy above](#pinned-dependencies-first-party-reusable-workflows))
- ✅ **Token-Permissions**: Minimal permissions in workflows
- ✅ **Security-Policy**: This file exists
- ✅ **SAST**: CodeQL and gosec enabled
- ✅ **Dangerous-Workflow**: No dangerous patterns
- ⚠️ **Branch-Protection**: Configure via GitHub UI (see above)
- ⚠️ **Code-Review**: Enabled via branch protection
