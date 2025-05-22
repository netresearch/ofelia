# Codex Agent Instructions

This repository is written in Go. Follow these rules when contributing:

## Formatting
- Format all modified Go files with `gofmt -w`. Unformatted code must not be committed.

## Vetting and Testing
- Run `go vet ./...` and `go test ./...` after changes. All commands should pass before committing.

## Documentation
- Update `README.md` or files in `docs/` when you change user-facing behavior.

## Commits
- Use semantic commit messages following the Conventional Commits style (e.g.,
  `feat:`, `fix:`, `docs:`) for all commits.
- Write a comprehensive commit message body that thoroughly describes every
  change introduced.

## Repository Hygiene
- Manage dependencies exclusively with Go modules.
- Do **not** vendor or commit downloaded modules. Avoid running `go mod vendor`.
- Ensure the `vendor/` directory is ignored via `.gitignore`.
