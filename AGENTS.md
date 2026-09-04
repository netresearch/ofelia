<!-- Managed by agent: keep sections and order; edit content, not structure. Last updated: 2026-06-03 -->

# AGENTS.md (root)

This file explains repo‑wide conventions and where to find scoped rules.  
**Precedence:** the **closest `AGENTS.md`** to the files you're changing wins. Root holds global defaults only.

## Global rules
- Keep diffs small; add tests for new code paths.
- Use semantic commit messages following Conventional Commits style (e.g., `feat:`, `fix:`, `docs:`).
- Write comprehensive commit message bodies that thoroughly describe every change introduced.
- Ask first before: adding heavy deps, running full e2e suites, or repo‑wide rewrites.
- Update `README.md` or files in `docs/` when you change user-facing behavior.

## Minimal pre‑commit checks
- Format Go code: `gofmt -w $(git ls-files '*.go')`
- Vet code: `go vet ./...`
- Run tests: `go test ./...`  
- Full lint check: `make lint`
- Security check: `make security-check`

## Local development
- Requires a running Docker daemon (start it with `sudo systemctl start docker` on systemd hosts, or `sudo service docker start` otherwise)
- Run the daemon with the bundled demo config: `go run . daemon --config example/ofelia.ini` — serves the web UI on `web-address` (default `:8081`, which binds all interfaces). The example config does not override it, so reach the UI at `http://127.0.0.1:8081/`; pass `--web-address 127.0.0.1:8081` to force loopback-only binding
- `example/ofelia.ini` ships working demo jobs (`run-date` runs `date` in alpine every 30s; `local-echo` runs on the host every 45s); the swarm and compose examples are commented out as they need extra infrastructure
- Web UI assets live in `static/ui/` (`styles.css`, `app.js`, `templates/`) and are embedded via `//go:embed ui/*` in `static/static.go`; new files added under `static/ui/` are picked up automatically (no registration needed). When a pattern matches a directory, `go:embed` walks it recursively, so `ui/*` also covers `ui/templates/` — files whose name starts with `.` or `_` are the exception and stay out
- After touching embedded assets, run `go build ./...` to confirm the embed still resolves
- Web package tests: `go test ./web/... -v -count=1` (`-count=1` bypasses the test cache)

## Go JSON serialization
- Struct fields with explicit `json` tags use the tag name (e.g., `json:"lastRun"` → `lastRun`)
- Struct fields **without** `json` tags serialize as the Go field name (capitalized: `Image`, `Container`)
- Always `grep 'json:"' web/server.go` before writing frontend code that reads API responses
- `apiJob.Config` is `json.RawMessage` from `json.Marshal(job)` — core structs lack json tags, so keys are capitalized

## CI & merge workflow
- ~26 CI checks: golangci-lint (140-char line limit), CodeQL, Trivy, govulncheck, mutation, unit/integration/fuzz (CodSpeed removed)
- Repo uses **GitHub merge queue** — `gh pr merge --delete-branch` is NOT supported
- Automated reviewers: github-actions (auto-approve), gemini-code-assist, Copilot (both COMMENTED — check all)

## SonarCloud
- Project `netresearch_ofelia` uses **Automatic Analysis** — no `sonar-project.properties`, no sonar CI step. Config lives in project settings via API/UI; committing scanner config files risks disrupting Automatic Analysis.
- Exclude a rule on a path: `POST api/settings/set` with `key=sonar.issue.ignore.multicriteria` — a multi-key setting: pass `fieldValues` with both companion fields, e.g. `--data-urlencode 'fieldValues={"ruleKey":"go:S3776","resourceKey":"**/*_test.go"}'` (verify via `api/settings/values`). Issue dispositions: `api/issues/do_transition` — on SonarCloud `accept` supersedes the retired `wontfix`; the allowed transitions per issue are listed in the issue's own `transitions` array from `api/issues/search`. Hotspots: `api/hotspots/change_status` — SonarCloud only accepts `REVIEWED` + `SAFE`/`FIXED`, no ACKNOWLEDGED.
- `gocognit` reproduces SonarCloud `go:S3776` cognitive-complexity numbers exactly; golangci-lint here runs `gocyclo` (cyclomatic), which does NOT catch S3776.
- Extracting a route constant named `*Token` (e.g. `pathAPICSRFToken`) trips gosec G101 (hardcoded credentials) in the GHAS code-scanning gate — suppress with `// #nosec G101 -- reason`. The standalone `go-check / gosec` and the GHAS `gosec` check are separate.
- Reusable workflows pinned `@main` are intentional (own org, post-trivy-action-incident policy) — the related hotspots are accepted, not third-party actions.

## Backlog — picking the next issue
- There are no priority labels. The signals that exist: **`upstream`** (12 of 24 open issues) marks work inherited from `mcuadros/ofelia` — such an issue describes that project's behaviour and is not automatically a defect here, so check the code before treating it as one. `bug` separates a defect from `feature`/`enhancement`; `security` outranks both.
- **Read the body, not the title, before proposing an issue as work.** Two of sixty issues state their own impact, and where one does, that grading is the answer rather than context: [#718](https://github.com/netresearch/ofelia/issues/718) rates itself "low/theoretical in practice … Filing for the record" and lists *document the reservation* as its first option — which is what closed it. Recommending such an issue as a real defect contradicts its reporter and needs a stated reason.
- An issue's **proposed solution** is the reporter's hypothesis from the day they wrote it; re-derive it from the code. Its **scope claims** ("affects X and Y") are a reading of the call graph, not a measurement.

## Deprecation policy
- Deprecated options keep working until a **major** release. The single source of truth is `deprecationRemovalVersion` in `cli/deprecations.go` (currently `v2.0.0`); the warning text is generated from it, and `cli/deprecations_test.go` asserts every entry uses it rather than a literal.
- **Extending the window beats dropping options in a non-major.** For [v1.0.0](https://github.com/netresearch/ofelia/releases/tag/v1.0.0) the three options `slack-webhook`, `poll-interval` and `no-poll` were scheduled for removal; the window moved to `v2.0.0` instead, so upgrading required no configuration change.
- Changing that constant is a **declared-value sweep**, and the prose surfaces do not follow automatically: `middlewares/slack.go`'s `// Deprecated:` doc comment (pkg.go.dev renders it), `CHANGELOG.md`, and `docs/`. Grep **without** an extension filter — the enforcing surfaces include dotfiles.

## What earns a major bump
- Removing a deprecated option, changing what an existing configuration key means, or dropping a supported input form. These are the changes that make a working config stop working.
- **Raising the minimum Go version is not one of them.** Users consume binaries and the `ghcr.io` image, not the module; the floor binds whoever compiles the source. [#824](https://github.com/netresearch/ofelia/pull/824) raised it to 1.27 without a major being proposed.
- New options, new job types and new endpoints are minors. Fixes that keep every documented input working are patches.

## Release process
- **Pushing the tag is the release.** `release.yml` triggers on `push` of a
  `v*` tag and calls `netresearch/.github/.github/workflows/release-go-app.yml@main`,
  which creates the GitHub release, the binaries, the SLSA provenance, the
  SBOMs and the `ghcr.io/netresearch/ofelia` image tags. `verify-release.yml`
  runs afterwards against the published result.
- **Never run `gh release create`.** CI owns creation, and under immutable
  releases a lightweight tag from that command burns the version name
  permanently. The order is: cut the release PR, merge it, tag `main`'s merge
  commit with `git tag -s vX.Y.Z`, push the tag, wait.
- `gh release edit vX.Y.Z --notes-file notes.md` afterwards is the supported
  way to replace CI's generated notes. `--notes-file` and `--repo` are the
  flags to use; the rest of that command's flags are blocked.
- `scripts/update-release-notes.sh` appends an "Included in this release"
  index linking the `released:vX.Y.Z` label filter. It exists and the labels
  are maintained, but no release currently carries that section — treat it as
  available, not as a step the flow depends on.
- Follow the narrative release notes style from previous releases: user-facing
  highlights first, then categorized changes, contributors `@mentioned` inline
  at the change they touched rather than in a section of their own.

## Dependencies
- `github.com/netresearch/go-cron` — maintained fork of robfig/cron with DAG engine, pause/resume, @triggered schedules
- Go version tracked in `go.mod` — CI reads from `go-version-file: go.mod`
- Update Go version in `go.mod` to fix stdlib vulnerabilities (govulncheck detects these)

## Index of scoped AGENTS.md
- `./cli/AGENTS.md` — command-line interface and configuration
- `./core/AGENTS.md` — core business logic and scheduling
- `./web/AGENTS.md` — web interface and HTTP handlers
- `./middlewares/AGENTS.md` — notification and middleware logic
- `./test/AGENTS.md` — testing utilities and integration tests

## Recurring friction notes
- `./docs/feedback/golangci-lint-cache-cross-worktree.md` — run `golangci-lint cache clean` before pushing if you use multiple sibling worktrees; stale cache entries from siblings get replayed as findings and block the `pre-push` hook.

## Repository hygiene
- Manage dependencies exclusively with Go modules.
- Do **not** vendor or commit downloaded modules. Avoid running `go mod vendor`.
- Ensure the `vendor/` directory is ignored via `.gitignore`.

## Archived repos
- `netresearch/node-vault` — archived, do not create PRs
- `netresearch/satis-git` — archived, do not create PRs

## When instructions conflict
- The nearest `AGENTS.md` wins. Explicit user prompts override files.
