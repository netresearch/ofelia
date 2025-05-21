# Codex Agent Instructions

This repository is written in Go. Follow these rules when contributing:

## Formatting
- Format all modified Go files with `gofmt -w`. Unformatted code must not be committed.

## Vetting and Testing
- Run `go vet ./...` and `go test ./...` after changes. All commands should pass before committing.

## Documentation
- Update `README.md` or files in `docs/` when you change user-facing behavior.
