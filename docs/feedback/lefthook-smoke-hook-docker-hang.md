# Pre-push smoke hook hangs in `core/adapters/docker` (WSL2)

The lefthook **pre-push** smoke hook

```
go test -short -timeout=60s ./core/... ./cli/... ./middlewares/...
```

hangs in the `core/adapters/docker` integration package on a WSL2 Docker box (a test
times out at ~60s → `panic: test timed out`), which blocks pushes even for changes that
touch no `core/` files.

When blocked:

1. Confirm the failure is that hang and your diff does not touch `core/`:
   `go test ./cli/... ./middlewares/... -count=1`.
2. If clean, `git push --no-verify` and rely on CI — CI is the authoritative gate.

Never disable or skip the docker tests. If your change *does* touch
`core/adapters/docker`, investigate the hang instead of bypassing it.
