# Project Governance

This document describes the governance model for the Ofelia project.

## Overview

Ofelia is an open source project maintained by [Netresearch](https://www.netresearch.de/).
The project follows a **Benevolent Dictator for Life (BDFL)** governance model with community input.

## Roles and Responsibilities

### Maintainers

Maintainers are responsible for the overall direction and health of the project:

- **@CybotTM** - Lead Maintainer
- **@netresearch/netresearch** - Netresearch Team

Maintainer responsibilities include:
- Reviewing and merging pull requests
- Triaging issues and feature requests
- Making architectural decisions
- Managing releases
- Enforcing the Code of Conduct

### Contributors

Anyone who contributes to the project through:
- Code contributions (pull requests)
- Documentation improvements
- Bug reports and feature requests
- Community support (answering questions)

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to contribute.

### Security Team

The security team handles vulnerability reports:
- **@netresearch/sec** - Security Team

See [SECURITY.md](SECURITY.md) for reporting vulnerabilities.

## Decision Making

### Day-to-day Decisions

- Maintainers make routine decisions (bug fixes, minor features)
- Pull requests require approval from at least one maintainer
- Security-sensitive changes require review from the security team

### Significant Changes

For major changes (breaking changes, new features, architectural decisions):

1. Open an issue or discussion to propose the change
2. Allow community feedback period (minimum 7 days for major changes)
3. Maintainers make final decision considering community input
4. Document decision rationale in the issue/PR

### Conflict Resolution

1. Discuss in the relevant issue/PR
2. If unresolved, escalate to maintainers
3. Lead maintainer has final decision authority

## Project Direction

### Roadmap

The project roadmap is maintained in:
- GitHub Issues (labeled with `enhancement`)
- GitHub Milestones for release planning

### Versioning

The project follows [Semantic Versioning](https://semver.org/):
- MAJOR: Breaking changes
- MINOR: New features (backwards compatible)
- PATCH: Bug fixes (backwards compatible)

## Communication

- **Issues**: Bug reports and feature requests
- **Pull Requests**: Code contributions and reviews
- **Discussions**: General questions and community interaction

## Amendments

This governance document may be amended by maintainers. Significant changes
will be announced and allow for community feedback.

---

*Last updated: November 2025*
