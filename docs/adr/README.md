# Architecture Decision Records

This directory contains Architecture Decision Records (ADRs) for Ofelia.

ADRs document significant architectural decisions, their context, and consequences.
They serve as a historical record of why certain decisions were made.

## Format

Each ADR follows this structure:
- **Title**: Short descriptive name
- **Status**: Proposed | Accepted | Deprecated | Superseded
- **Context**: What prompted this decision
- **Decision**: What we decided
- **Consequences**: What this means for the project

## Index

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [ADR-001](./ADR-001-docker-registry-authentication.md) | Docker Registry Authentication | Accepted | 2025-12-10 |
| [ADR-002](./ADR-002-security-boundaries.md) | Security Boundary Definition | Accepted | 2025-12-17 |

## Creating New ADRs

1. Copy the template below
2. Use next sequential number (ADR-NNN)
3. Name file `ADR-NNN-short-title.md`
4. Add to the index above

### Template

```markdown
# ADR-NNN: Title

**Status**: Proposed | Accepted | Deprecated | Superseded by ADR-XXX
**Date**: YYYY-MM-DD
**Authors**: Name(s)

## Context

[What is the issue that we're seeing that motivates this decision?]

## Decision

[What is the change that we're proposing and/or doing?]

## Consequences

### Positive
- [Benefit 1]
- [Benefit 2]

### Negative
- [Drawback 1]
- [Drawback 2]

### Neutral
- [Other implications]

## References

- [Related document or issue]
```
