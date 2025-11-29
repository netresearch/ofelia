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
