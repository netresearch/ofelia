# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **`default-user` and a job's own `user` are documented as the distinct
  states they have.** The setting had half a sentence per job type, and
  an empty value means something different at the two levels — which the
  documentation did not say and is the easy mistake. For
  `[global] default-user`: absent yields `nobody`, empty and `default`
  both yield the container's own user, anything else is taken literally.
  For a job's own `user`: absent or empty *inherits* whatever the global
  resolved to, and only `default` bypasses it. Both are now tables in
  `docs/CONFIGURATION.md`, and the three per-job descriptions in
  `docs/jobs.md` say which of the two they are.

  The reservation of `default` is written down for the first time: a
  container user of that name cannot be selected at either level. The
  global has a second spelling that avoids the collision — an empty
  value — while a job's `user` has none, since empty already means
  inherit ([#718](https://github.com/netresearch/ofelia/issues/718)).

- **The minimum Go version is 1.27.** The toolchain moved to go1.27.0 in
  [v0.30.0](https://github.com/netresearch/ofelia/releases/tag/v0.30.0)
  while `go.mod` kept a `go 1.26` directive, which is what the
  Go-version badge reads. That directive is not just a floor for who can
  build: Go compiles a module declaring an older version with that
  version's compatibility defaults, so the released binary carried
  `DefaultGODEBUG=tracebacklabels=0,x509sslcertoverrideplatform=0` — the
  two behaviours Go 1.27 changed, pinned back — despite being built by
  go1.27.0. Raising the directive drops those pins and makes the 1.27
  language features available. Building from source now needs a 1.27
  toolchain, which `toolchain go1.27.0` already fetches unless
  `GOTOOLCHAIN=local` forbids it; nothing imports this module as a
  library, and the published images and binaries are unaffected either
  way.

## [1.0.0] - 2026-09-03

The first stable release. The number is the commitment it implies: from here, a
breaking change to the documented HTTP API or to the INI and label
configuration takes a major bump, and the "pre-1.0" allowance the two
source-only breaks below were taken under no longer applies.

Most of the surface is the rebuilt dashboard and the API it runs on — an
aggregate `/api/dashboard` endpoint, response compression, ETagged assets,
per-job sparklines and duration stats, search and sorting. The API grew a gate:
jobs owned by the INI file or by Docker labels can no longer be created,
updated or deleted through it, and jobs it does own now survive a restart.

Upgrading needs no configuration change. `slack-webhook`, `poll-interval` and
`no-poll` were scheduled for removal here and still work; that window now runs
to v2.0.0.

### Added

- **Origin badges and honest delete buttons.** Config-owned jobs (INI or
  Docker labels) show an `ini`/`label` badge explaining they are deleted
  at their source, and the UI no longer offers them a delete button that
  could only end in a 403.
- **Running indicator** — a pulsing teal dot marks a job mid-run
  (respects `prefers-reduced-motion`).
- **The Failing stat card toggles a failed-only table filter**
  (keyboard-accessible, `aria-pressed`).
- **Result sparkline per job** — a "Last runs" column with one square per
  recent execution (green/yellow/red, tooltip with time and outcome),
  fed by a new additive `recentRuns` field on job payloads.
- **The Duration cell shows last / avg / max** over the job's recent
  completed runs as three labeled lines — a slowing job shows up at a
  glance.
- **Stat cards above the jobs table**: active jobs (with paused count),
  jobs whose last run failed (red accent + ⚠ only when non-zero), and the
  nearest upcoming run with a countdown. Computed from the same dashboard
  poll — no extra requests.
- **Job table search and sorting; sortable history.** A search input above
  the jobs table matches name, command, schedule, and the displayed Last
  Run/Duration formats; header clicks sort jobs (name, schedule, command,
  last run, duration — raw values, so durations sort numerically and ISO
  timestamps chronologically) and history (date, duration, error), with
  SVG chevron indicators. Both are reusable opt-in helpers
  (`createTableSearch`, `createTableSort` + `data-sort` header flags) and
  re-render purely from cached data — zero API calls per keystroke or
  sort click.
- **Response compression, zstd or gzip.** Clients advertising a supported
  codec get compressed pages, assets, and API responses (first page load
  ~140 kB → ~28 kB; each dashboard poll 5.8 kB → 1.6 kB). The wrapper
  enables zstd next to gzip and prefers it at equal q-values, so Chrome,
  Edge and Firefox — which send `Accept-Encoding: gzip, deflate, br,
  zstd` — receive `Content-Encoding: zstd`, and clients that advertise
  gzip but not zstd (Safari below 26, older monitoring scripts) receive
  gzip. Clients that advertise no encoding at all, `curl` with no flags
  among them, get an identity response. Delegated to
  `klauspost/compress/gzhttp`, which handles Accept-Encoding qvalues,
  content sniffing, bodiless statuses, and ranged requests.
- **`GET /api/dashboard` — aggregate snapshot endpoint.** Returns jobs,
  disabled, removed, and config in one response (optionally a job's history
  via `?history=<name>`). Additive: the per-resource endpoints are
  unchanged. The web UI now polls this single endpoint per 5s tick instead
  of 4–5 separate requests, which used to exhaust the 100-requests-per-
  minute rate limit with two dashboard tabs open.

- **UI development mode.** When `OFELIA_UI_DEV_DIR` names a directory, the web
  server serves UI assets from it on every request and re-parses the page
  templates per request, so an edit is visible on the next browser reload
  without rebuilding the binary. Unset in production; the embedded assets
  remain the default.
- **Build version in the footer.** Fetched once from the auth-exempt `/health`
  endpoint; release builds show the goreleaser version, dev builds show `dev`.
- **`POST /api/jobs/create`/`update` accept `maxRuntime` for `type=run` jobs.** Previously the only way to bound how long an API-created run-job could execute was the scheduler's fixed 24h default (`defaultJobMaxRuntime`, issue #638) — `config.ini`/label jobs could already set a per-job `max-runtime`, but the API had no field for it. `maxRuntime` takes a Go duration string (e.g. `"30m"`), mirrors the `[job-run] max-runtime` syntax, and round-trips through the state file so a persisted job keeps its override across daemon restarts ([#789](https://github.com/netresearch/ofelia/pull/789)). `"0s"` is equivalent to omitting the field: it falls back to the 24h default rather than meaning "unlimited".

### Changed

- **BREAKING (API behavior):** `POST /api/jobs/update` now returns
  `403 Forbidden` for jobs that came from INI config or Docker labels,
  mirroring the delete gate, where it previously answered `200 OK`.
  Scripts that edited config-owned jobs through the API must edit the
  source config (or the container labels) instead. Pre-fix an update
  silently overrode such a job in memory until the next config sync —
  and rewrote the job's origin, after which the delete gate could be
  bypassed and a label job deleted. The UI shows edit and delete on
  those jobs as disabled buttons with a tooltip naming the source.
- **BREAKING (API behavior):** `POST /api/jobs/create` now returns
  `403 Forbidden` for a name owned by INI config or Docker labels, where
  it previously answered `201 Created`. The gate is on the name, not on a
  registered job: a config job with an empty or malformed schedule holds
  no cron entry, so nothing stopped a create from taking its name,
  replacing it with a caller-chosen job and recording `origin: api` —
  after which the update and delete gates no longer recognized it either.
- **BREAKING (runtime behavior):** API-created exec, compose and local
  jobs get the config decoder's struct-tag defaults. Only run jobs did.
  Most importantly `HistoryLimit` was 0, which makes the job retain every
  execution forever — each holding up to two 10 MB output buffers — so a
  frequently-run API-created job exhausted daemon memory. The same gap
  existed on the state-file loader, so the leak came back on the first
  restart even for a job created after the fix; both boundaries apply the
  defaults now. The other defaults come with it, and one of them changes
  what an existing setup does: `AllowParallel` is true, so an
  API-created job whose run outlasts its interval now overlaps itself
  where the run used to be skipped — the same behavior INI-defined and
  label-defined jobs have always had. Also `RetryDelayMs` 1000 and
  `compose.yml` as a compose job's default file.
- **The deprecation window for `slack-webhook`, `poll-interval` and `no-poll` runs to v2.0.0.** All three were scheduled for removal in v1.0.0 and still work. The window was extended rather than the options dropped, so upgrading to `v1.0.0` does not require a configuration change. `poll-interval` and `no-poll` are migrated automatically to `config-poll-interval` / `docker-poll-interval` and the polling defaults; `slack-webhook` is not, and a `[webhook "name"]` section with `preset = slack` replaces it. The removal target in the warning text moved with the window, so a `v1.0.0` daemon no longer reports that an option "will be removed in v1.0.0" while running it.
- **The health report is served from a snapshot.** `GetHealth` called
  `runtime.ReadMemStats` per request, which stops the world, and `/ready`
  is exempt from rate limiting — an unauthenticated caller could force a
  GC pause per request. The periodic system check already takes that
  reading once per interval; the report now uses it. Same wire shape.
- **Responses below 1 KiB are no longer compressed.** Below roughly a
  packet's worth of payload the codec framing plus the CPU cost buys no
  fewer bytes on the wire — `/live`'s two-byte `OK` is the clearest case.
  gzhttp's default threshold applies instead of `MinSize(0)`.
- **The embedded UI assets carry an ETag**, so a browser revalidating one
  gets a 304 instead of a full, freshly compressed body. Embedded files
  have no ModTime, so nothing could 304 before.
- **The open job's history is sent only when it changes.** A response
  carrying history also carries `historyFingerprint`; a client echoing it
  as `&historyFp=` gets `history` omitted while it still matches, instead
  of the full stdout and stderr of every retained run on every 5s tick.
- **`/api/jobs/removed` no longer carries `recentRuns`.** Nothing in the
  removed tab reads it.
- **BREAKING (source-only, pre-1.0):** `core.DockerProvider` gains
  `CopyContainerLogs`, needed to demux container output server-side.
  Downstream Go code implementing the exported interface fails to
  compile until it adds the method; permitted under
  [SemVer §4](https://semver.org/#spec-item-4) for the current 0.y.z
  line. Users of the provided implementations are unaffected.
- **BREAKING (source-only, pre-1.0):** `Scheduler.UpdateJob` now updates
  a disabled job instead of returning `ErrJobNotFound`, so editing a
  paused job no longer resumes it. Callers that relied on the error to
  detect "not scheduled" must check the disabled state explicitly.
- **Only `/live` and `/ready` bypass the rate limiter.** A probe answered
  429 reads as unhealthy and gets the daemon restarted, and both probes
  are cheap — a constant string and a copy of the check map the
  background loop maintains. `/health` and `/healthz` stay token-free but
  counted: they answer with the full report — every check, the version
  and the goroutine count — which is both more work per request and more
  than a probe needs to know. Every other request is counted too, static
  assets included. The UI stays inside the budget by polling one
  aggregate endpoint per tick rather than by being exempted.
  `/api/login` keeps its own stricter login limiter.
- **The dashboard renders with site data blocked.** Both the pre-paint
  script and `app.js` read `localStorage` at the top level, and a browser
  that blocks site data throws on the property itself — the script
  aborted before the first render and the page stayed blank. Reads and
  writes are guarded now; without storage the UI falls back to the
  defaults and keeps preferences for the page view only.
- **A hidden browser tab stops polling** (Page Visibility API) and
  refreshes immediately when it becomes visible again — n open dashboard
  tabs cost one tab's request budget.
- **Short pages pin the footer to the bottom edge** (min-height 100dvh
  flex column).
- **The web UI is assembled from templates and separate assets.** The former
  single-file `static/ui/index.html` is split into `styles.css`, `app.js`, and
  Go `html/template` partials (`templates/layout.html` plus one file per tab),
  rendered server-side at `GET /`. No behavior or dependency change; still
  vanilla CSS/JS with no build step.
- **Job history opens in a modal dialog** instead of a panel under the jobs
  table. Close via the header button, Esc, or a backdrop click. The dialog is
  anchored to the top of the viewport so the 5s refresh does not make it jump,
  and its padding follows the compact/comfortable density setting.
- **Run output renders in a full-width subrow** of the history table instead
  of inside the Output column. Expanding output no longer changes column
  widths; long output wraps and scrolls in its own box; the run's row is
  highlighted while open; expanded state still survives the 5s refresh, keyed
  by execution timestamp and scoped to the shown job.
- **The tab bar moved into the sticky nav**, left-aligned next to the brand;
  the footer spans the full page width. Both bars share the same horizontal
  padding via the `--layout-pad-x` CSS variable, and repeated separator
  borders use `--border-thin`. The nav dropped its `<ul><li>` wrappers —
  single-item lists carried no semantics, and Pico's `nav li` padding
  ignored the density setting.
- **Brand primary is teal `#2f99a4`** (was Pico blue), set via the
  `--pico-primary*` token family for light, dark, and auto theme modes.
- **The dark theme background is neutral graphite** (`#181b1e`, cards
  `#22262a`) instead of Pico's blue-tinted default, so the teal primary is
  the only cool hue on screen. Form inputs and dropdowns follow the same
  graphite family (`--pico-form-element-*` tokens).
- **Job-row action buttons are soft teal chips with SVG icons.** The emoji
  glyphs (▶ ✎ ⏸ 🗑) became uniform inline stroke SVGs on a 24px grid,
  colored via `currentColor`; delete is red-tinted at rest. Icon colors
  come from `--action-fg`/`--action-del-fg` with per-theme shades.
- **Deleting a job asks for confirmation**, and API failures are no longer
  silent: a reusable bottom-right toast (`toast.success/error/info`) shows
  the server's message — notably the 403 explaining that INI-owned jobs
  must be deleted in the config file. Run, pause, resume, and delete show
  success toasts.
- **Job rows signal their clickability**: pointer cursor, hover tint, and
  the job name is a link-styled button, so keyboard users can Tab to it
  and open the history with Enter.
- **Tables are striped.** Pico's `.striped` variant on all four tables,
  with the stripe color raised to 6% of the contrast color (Pico's ~4%
  alpha was invisible on the graphite dark background). The history table
  stripes in pure CSS by run/subrow pairs — the output subrow is always
  rendered, shares its parent run's background, aligns with the Date
  column, and gets density-scaled padding when open. The status-dot
  column has a fixed narrow width (`--dot-col`) so rows stay aligned.
- **The rendered page and stylesheet pass the W3C Nu validator** (checked
  locally via the `ghcr.io/validator/validator` Docker image).

### Fixed

- **A flaky mock made unrelated pull requests fail.**
  `mock.EventService.Subscribe` buffers a configured subscribe error and
  then closes both channels, so once its goroutine had run the pending
  error and the closed event channel were ready in the same instant —
  and `SubscribeWithCallback` selected between them at random, returning
  `nil` whenever the closed-channel arm won. Measured at 89 losses in
  200 runs with the goroutine given a head start, which is what a loaded
  runner arranges; `TestEventService_SubscribeWithCallback_SubscribeError`
  failed that way on a PR touching no part of that package. The end of
  the stream is now only reported as success once no error is pending,
  and an error channel that closed without carrying one is dropped from
  the select instead of competing with events still buffered behind it
  ([#820](https://github.com/netresearch/ofelia/issues/820)).
- **Unauthenticated `/api/*` requests are counted by the rate limiter.**
  The limiter sat inside the auth middleware, so a request answered with
  `401` never reached it: token guessing against `/api/*` was the one
  kind of traffic it never saw, while the same address was metered on
  every static asset it fetched. Measured before the fix, 400
  unauthenticated requests to `/api/jobs` from one address produced 400
  `401`s and no `429` at any point. The limiter now wraps auth from
  outside. Nothing else changes: auth only guards `/api/*`, so the
  rendered page and the static assets were already counted, and the
  orchestrator probes `/live` and `/ready` were exempt before the move
  and stay exempt
  ([#804](https://github.com/netresearch/ofelia/issues/804)).
- **API-created run jobs inherit `[global] max-runtime`.** An INI
  `[job-run]` section with no per-job bound picks up the operator's
  global at config load; a run job created over the API skipped that
  rung and landed on the scheduler's 24h constant, so one daemon applied
  two different bounds to the same job type depending on where the job
  came from. The divergence was invisible until an operator overrode the
  global, since the constant and the documented default are both 24h —
  which is to say it appeared exactly when someone had expressed an
  intent. Creation and restore-from-state-file both inherit now, and the
  state file keeps what the caller asked for rather than the resolved
  value, so a changed global reaches restored jobs at the next start the
  way it reaches INI ones. `"0s"` still means the same as omitting the
  field ([#806](https://github.com/netresearch/ofelia/issues/806)).
- **The OpenAPI job-request schema is now held to what the handler
  accepts.** Nothing validated `docs/openapi.yaml`, so it drifted: it
  advertised a job type `service-run` that `jobFromRequest` rejects and
  `jobType` never emits, and it marked `type` as required although an
  omitted type is a valid request that yields a local job. Both were
  found by reading the diff, which is not a mechanism. A test now
  compares the documented `type` enum against the tokens the real handler
  dispatches on, and fails rather than skips when it cannot find the enum
  ([#816](https://github.com/netresearch/ofelia/issues/816)).
- **A delete landing mid-update can no longer leave a ghost job.**
  `RemoveJob` drops the cron entry before it takes the scheduler lock, so
  it can complete inside `UpdateJob`'s window; the update then reinserted
  the deleted name into the by-name map, where it had no cron entry, never
  fired again, and was still reported as live by the API. The update also
  performed its last fallible step after rewriting its state, so a
  failure reported an update that had in fact taken effect.
- **A failed UI render answers 500 instead of a half-rendered 200.** The
  page was executed straight into the response, so a template failing
  partway through — reachable under `OFELIA_UI_DEV_DIR` — committed a 200
  and then appended the error text to the partial page.
- **The dashboard job lists come from one snapshot.** Read through three
  separate locks, a job disabled or removed between two of them appeared
  in two lists at once and showed as two rows until the next tick.
- **Server-time display keeps the zone offset.** It was sliced off, and
  nothing else in the UI reveals the server's zone, so a timestamp
  correlated against server logs read as local time.
- **Tooltip text reaches screen readers and dismisses on Escape.** The
  reason an edit or delete button is inert lived only in a data
  attribute; the bubble now carries `role="tooltip"` and its anchor
  `aria-describedby` (WCAG 2.2 SC 1.4.13 for the dismissal).
- **A long log stays where you scrolled it.** Scrolling to the bottom of a
  run's output threw the reader back to the top on the next 5s refresh,
  which made a long log effectively unreadable. The refresh rebuilds the
  history table, so the element that owns the scrollbar was replaced; the
  scroll offset is now carried across the rebuild
  ([#808](https://github.com/netresearch/ofelia/issues/808)).
- **The Exec-mode switch reports its own state.** `role="switch"` on the
  create/edit form's Exec checkbox overrides the input's implicit role,
  and with it the checked state the browser would otherwise expose — so
  assistive technology read the switch as permanently off however the
  user set it. `aria-checked` is now declared and kept in sync
  ([#811](https://github.com/netresearch/ofelia/pull/811)).

## [0.30.0] - 2026-08-26

A maintenance release: Go 1.27, the whole dependency graph brought current,
and the codebase modernized to the Go 1.26 idioms.

### Changed

- Go toolchain updated to go1.27.0; the supported minimum stays Go 1.26 ([#800](https://github.com/netresearch/ofelia/pull/800)).
- Scheduler dependency netresearch/go-cron updated to [v0.16.0](https://github.com/netresearch/go-cron/releases/tag/v0.16.0), its first release with the Go 1.26 floor ([#802](https://github.com/netresearch/ofelia/pull/802)).
- All Go dependencies updated across the module graph — docker/cli 29.7.2, emersion/go-smtp 0.25.0, moby/moby/client 0.5.1, testify 1.12.1, golang.org/x/crypto 0.55.0, golang.org/x/text 0.41.0 and indirect moves ([#801](https://github.com/netresearch/ofelia/pull/801)).

### Internal

- Codebase modernized via `go fix` and manual passes: `strings.Cut`, `slices.Backward`, `maps.Copy`, `errors.AsType[T]`, `atomic.Int32` in tests, and six stale `//nolint:goconst` directives removed; no behavior change ([#800](https://github.com/netresearch/ofelia/pull/800)).

## [0.29.1] - 2026-08-12

A security release. Jobs defined through Docker container labels could carry privilege-bearing keys the label-security policy did not cover, letting an untrusted self-labeling container escalate against the host or read another container's secrets — in the default configuration. See [GHSA-h7m7-v83x-vfp3](https://github.com/netresearch/ofelia/security/advisories/GHSA-h7m7-v83x-vfp3).

### Security

- **Label-sourced jobs can no longer smuggle `privileged`, `env-file` or `env-from` past the host-escalation policy.** `allow-host-jobs-from-labels` stripped host bind mounts from `job-run` / `job-service-run` ([#462](https://github.com/netresearch/ofelia/issues/462)) but never covered these three keys, and `job-exec` was not routed through the policy at all. So a container labelling itself could run a `privileged` `docker exec` (a container-escape primitive), read a file from ofelia's own filesystem view into the job environment (`env-file`), or copy another container's entire environment (`env-from`) — all with the policy in its default, off state. These keys are now stripped from every label-sourced job, matched by normalized key so casing and separator variants are caught, unless `allow-host-jobs-from-labels=true`; the strip runs on both the initial-load and the live container-reconcile paths and logs a `SECURITY POLICY VIOLATION` per stripped key. INI configuration is trusted and unaffected ([GHSA-h7m7-v83x-vfp3](https://github.com/netresearch/ofelia/security/advisories/GHSA-h7m7-v83x-vfp3), [#791](https://github.com/netresearch/ofelia/pull/791)).

## [0.29.0] - 2026-08-03

A release about ofelia telling the truth about itself. A job the scheduler
refused was reported as scheduled, `/health` claimed health it had never
established, `validate` returned success on a config it had not checked, and
shutdown ended the process before the hooks that stop the web server had run.
Each of those looked fine from the outside, which is what made them worth
fixing.

Three changes alter behaviour you may be relying on. `ofelia validate` now
exits non-zero when validation fails, so a pipeline step that silently passed
will start failing — that is the point, but check yours before upgrading. That
same command now also validates the jobs, so a config that passed before may
not. And `/health` can now answer `degraded`; `/ready` is unchanged and still
answers 200 for it.

### Added

- **`ofelia validate` now validates the jobs, not just the `[global]` section.** It checked global keys and stopped there, so a `[job-run]` without an `image` or a `[job-exec]` without a `container` passed the gate and then failed on every tick at runtime. Each job type is now checked for the fields its runtime actually requires, and the schedule and command are required everywhere ([#778](https://github.com/netresearch/ofelia/pull/778)).

### Fixed

- **`/health` reports `degraded` while a configured job is not scheduled.** The scheduler check was a stub that returned `healthy` unconditionally, with a comment saying a real implementation would check the scheduler. So a daemon whose job had a mistyped schedule — a job that never fires — served a green `/health`, which is the probe the integration docs tell operators to point a container healthcheck at. The check now names every job the scheduler refused. `/ready` still answers 200 for `degraded`: one job with a typo should not take a daemon out of rotation while its other jobs keep running. A corrected config reloaded at runtime clears the complaint without a restart ([#780](https://github.com/netresearch/ofelia/issues/780)).
- **Shutdown no longer ends the process before its hooks have run.** The daemon watched the channel that closes when shutdown *starts*, not when it finishes, so everything after the first priority group was killed mid-flight — the web server was never stopped gracefully despite having a hook registered to do it, cutting any request in progress. Measured on a release build, `SIGTERM` reached the end of shutdown in 0 of 5 runs before the fix and 5 of 5 after ([#781](https://github.com/netresearch/ofelia/pull/781)).
- **`/health` reported a version of `1.0.0` for every build ever shipped.** It was hardcoded at the call site, so the endpoint could not be used to tell which ofelia was answering. It now reports the running build, or `dev` when built without ldflags ([#781](https://github.com/netresearch/ofelia/pull/781)).
- **A job the scheduler rejects is now reported as such instead of being counted as running.** Five registration errors were discarded, and the startup line reported the number of jobs in the *config* rather than the number actually scheduled — so an operator saw a healthy daemon and a job that silently did nothing. The rejection is now logged at error level, naming the job, and the count describes what is really scheduled ([#777](https://github.com/netresearch/ofelia/pull/777)).
- **A failing command now leaves a non-zero exit status.** `ofelia validate` and every other subcommand returned 0 after logging the error, so `ofelia validate … || exit 1` in a pipeline never fired and a broken config passed the gate that exists to stop it ([#771](https://github.com/netresearch/ofelia/pull/771)).
- **`ofelia --config=x daemon` is accepted, not only `ofelia daemon --config=x`.** `--config` and `--log-level` were pre-parsed out of argv, which made them look global while the parser rejected them in the position a user would naturally write them ([#776](https://github.com/netresearch/ofelia/pull/776)).
- **Web credentials are only required when web authentication is enabled.** Validation demanded them unconditionally, so a config with the web UI open and unauthenticated — the default — failed a check it should have passed ([#775](https://github.com/netresearch/ofelia/pull/775)).
- **A container without a `Config` no longer crashes the daemon.** Two places dereferenced that pointer unguarded while converting Docker's response, which panics for any container the daemon reports without one ([#768](https://github.com/netresearch/ofelia/pull/768)).
- **An expanded job output no longer collapses on its own.** The web UI refreshes every five seconds, and with a job's history panel open that refresh rebuilt the whole table from scratch. Every `<details>` element was re-created without its `open` attribute, so any output a user had expanded snapped shut within five seconds of opening it — long enough to start reading, not long enough to finish. The history table now records which outputs are expanded before it re-renders and restores them afterwards, keyed by the execution's timestamp rather than its row position, so an expanded output also stays open when a new run appears above it. Only the user collapses an output now ([#764](https://github.com/netresearch/ofelia/issues/764)).

### Documentation

- **The documented health endpoints did not exist.** `docs/API.md` and `docs/PROJECT_INDEX.md` described `GET /health/liveness` and `GET /health/readiness`; the daemon serves `/health`, `/healthz`, `/ready` and `/live`. Both response shapes were wrong too. The OpenAPI description of `/health` did not match the served body either — it named `uptime` instead of `uptimeSeconds`, described `checks` as booleans where each is an object, omitted `timestamp` and `system`, and documented a 503 that `/health` never returns. All four endpoints are now specified, with shared `HealthResponse`, `HealthCheck` and `SystemInfo` schemas.
- **`durationMs` in the health report carries nanoseconds.** It serialises a Go `time.Duration`, which marshals as nanoseconds, so a local Docker ping reads `8332586` rather than `8`. The unit is now stated wherever the field is documented. The field name remains as-is; renaming it would break every existing consumer.

### CI

- The e2e shutdown test asserted on the substring `"graceful shutdown"`, which also matches `"Starting graceful shutdown"` — the line logged *before* the first hook. It stayed green while shutdown was broken, and now requires the completion line ([#781](https://github.com/netresearch/ofelia/pull/781)).
- Test results are reported to Codecov Test Analytics ([#769](https://github.com/netresearch/ofelia/pull/769)).
- The zizmor policy that exempts first-party reusable workflows from the pin rule now comes from the organisation-level reusable rather than a copy in this repository. It was added here in [#766](https://github.com/netresearch/ofelia/pull/766) and removed again in [#783](https://github.com/netresearch/ofelia/pull/783) once the reusable supplied it — a local file takes precedence over the fetched one, so leaving the copy behind would have pinned this repository to an ageing policy.

## [0.28.1] - 2026-07-28

A documentation and tooling release. The one change that reaches a running
deployment is the mail template; the rest corrects what the documentation
promised and makes the checks that should have caught it able to fail.

### Security

- **Secret scanning matched nothing.** `.gitleaks.toml` declared an allowlist and no rules, and a gitleaks config file *replaces* the built-in ruleset unless it extends it — so every scan reported "no leaks found" regardless of input. The config now extends the defaults. Verified by planting a token: it is reported with the fix and not without it. Turning the scan on surfaced four documentation examples, now unmistakable placeholders. One of them, an ntfy token in `docs/webhooks.md` and `middlewares/presets/ntfy-token.yaml`, has been in public history since 2025-12; it is not a live credential.

### Fixed

- **Notification mails no longer contain invisible characters.** The HTML mail body carried five U+200B zero-width spaces around the job name, duration and command. They shipped in every notification and are a known spam-filter signal.
- **The documentation described 37 configuration keys that do not exist.** Ofelia ignores an unrecognized key without warning, so an operator who pasted these snippets got none of the promised behavior. Git history shows none of them was ever implemented. The dangerous ones were in `SECURITY.md` under container hardening — `memory`, `memory-swap`, `cpu-shares`, `cpu-quota`, `capabilities-add`, `capabilities-drop`, `dns`, `tmpfs` — presented as the way to constrain a job, while every line was discarded. **If you set any of them, your jobs were never constrained.** That section now states ofelia has no such keys and shows where the limits belong: on the Compose service for exec jobs, on the daemon for run jobs, per [ADR-002](docs/adr/ADR-002-security-boundaries.md). Also corrected: `max-runtime` is not available on `job-exec` and a `[global] max-runtime` does not reach it; `timeout`, `delay` and `max-concurrent-jobs` exist nowhere; `user` is not a `job-local` key; `job-compose` takes only `file`, `service` and `exec`.
- **The release-verification instructions could not work.** Every command in `SECURITY.md` was wrong: the wrong signature file extensions, a signer workflow that does not exist, and a verifier pointed at assets no release ships. They are corrected and were executed against the published v0.28.0. The container image tag also drops the `v` that the release tag keeps — `v0.28.0` publishes `ghcr.io/netresearch/ofelia:0.28.0` — which the previous `<TAG>` placeholder hid behind a manifest-not-found error.
- Nine packages, including the module root, rendered "There is no documentation for this package" on pkg.go.dev. Every package and every exported symbol is now documented.

### Changed

- Error messages now start lowercase, matching Go convention and the rest of the codebase. Anything matching on the leading capital of a message such as `Docker image cannot be empty` needs adjusting.

### CI

- The linters could not fail on much. Blanket staticcheck exclusions hid the mail template's invisible characters; golangci-lint capped output at 50 issues per linter, so a regression could hide behind the cap; and findings on a line another linter had already flagged were dropped. All three are off, and the checks run against the integration and e2e build tags as well.
- New gates, each verified by breaking it: documented INI snippets are parsed with the real parser, so a renamed key cannot leave the docs behind; every HTTP route is held to a declared authentication expectation, closing the gap where a route registered outside `/api/` shipped reachable without a token; and each published release is re-verified with the commands `SECURITY.md` hands to users.

## [0.28.0] - 2026-07-27

### Security

- The Docker SDK moved from the frozen `github.com/docker/docker v28.5.2+incompatible` to the maintained split modules `github.com/moby/moby/client` and `github.com/moby/moby/api`. `govulncheck` now reports **zero** findings for this codebase, down from four: [GO-2026-5668](https://pkg.go.dev/vuln/GO-2026-5668) and [GO-2026-5617](https://pkg.go.dev/vuln/GO-2026-5617) (`docker cp` race conditions), [GO-2026-4887](https://pkg.go.dev/vuln/GO-2026-4887) (AuthZ plugin bypass) and [GO-2026-4883](https://pkg.go.dev/vuln/GO-2026-4883) (plugin-privilege off-by-one). None of the four had a fix on the v1 import path — upstream ended releases there, so leaving it was the only remedy. All four were reachable only through `init()` chains and were previously assessed as not exploitable in Ofelia's deployment shape, which is why this was deferred until `github.com/docker/cli` completed its own move to `moby/moby/client` (v29.6.2 imports it in 361 files against 7 still on the old path, and the `cli/config` subtree Ofelia depends on is clean of it). Closes [#667](https://github.com/netresearch/ofelia/issues/667). The new SDK reshaped every client call, so the migration was audited for behavior drift rather than assumed equivalent: two places where it would have altered runtime behavior — `job-exec` jobs combining `console-height` / `console-width` with `tty = false`, and startup pinging a daemon whose API version is already pinned — were caught and corrected before release, so both behave exactly as they did in 0.27.1.

### Changed

- **Docker Engine 19.03 or newer (API v1.40+) is now required.** The new client enforces a minimum API version and refuses to negotiate below it, where the previous client had no floor and simply clamped down to whatever the daemon reported. Ofelia has no documented minimum until now; in practice several features already required v1.42 (Engine 20.10, released 2020-12), so only daemons older than 2019 are affected.
- An invalid API version now fails at startup instead of later. `DOCKER_API_VERSION` and `[docker] version` are validated when the client is built, so a typo reports `invalid API version (…)` immediately; the previous client accepted any string and let the failure surface at request time. A leading `v` (`v1.44`) is now also accepted, where it used to be passed through unusable.
- `Network.Containers` is no longer populated when listing networks — the Docker list endpoint does not return per-network endpoints, and the new API types reflect that. Inspecting a network still returns them. No Ofelia feature reads the field from list results.

## [0.27.1] - 2026-07-27

### Fixed

- `type=run` jobs created through the web API or restored from the state file now remove their container after each execution, matching the behavior `config.ini` users already got. `RunJob.Delete` only received its `"true"` default via the config decoder's `default` struct tag, but `newRunJobFromRequest` (`web/server.go`) and `buildPersistedRunJob` (`cli/daemon.go`) construct the job directly and bypass that decoder, leaving `Delete` at its zero value `""` — which `deleteContainer` reads as `false` through `strconv.ParseBool`. Every API-created run job therefore left its container behind, and the next scheduled execution failed with `job run: creating container: create container "<name>": resource conflict`. Since `jobRequest` exposes no `delete` field, there was no way to work around it at the call site. Both construction paths now set the default explicitly, each covered by a regression test. ([#745](https://github.com/netresearch/ofelia/pull/745))

### Dependencies

- Go toolchain bumped 1.26.4 → 1.26.5 (`go.mod` plus the `make lint` / `make lint-fix` `GOTOOLCHAIN` pins). This clears [GO-2026-5856](https://pkg.go.dev/vuln/GO-2026-5856), an Encrypted Client Hello privacy leak in `crypto/tls` that `govulncheck` reported as reachable from the Docker client's TLS dialer. Direct and indirect modules were refreshed via `go get -u all`: `docker/cli` 29.5.3→29.6.2, `docker/go-connections` 0.7.0→0.8.1, `golang.org/x/crypto` 0.53.0→0.54.0, `golang.org/x/text` 0.38.0→0.40.0, `golang.org/x/term` 0.44.0→0.45.0, `golang.org/x/sys` 0.46.0→0.47.0, plus `docker-credential-helpers` 0.9.8, `felixge/httpsnoop` 1.1.0, `gabriel-vasile/mimetype` 1.4.15, `go-logr/logr` 1.4.4 and `leodido/go-urn` 1.5.0. Every module linked into the binary is on its latest release; the modules `go list -m -u all` still reports as outdated are test-dependencies-of-dependencies that MVS resolves but the binary never links. The remaining `govulncheck` findings are the unfixable upstream moby advisories on `docker/docker` v28.5.2 — [GO-2026-5668](https://pkg.go.dev/vuln/GO-2026-5668) and [GO-2026-5617](https://pkg.go.dev/vuln/GO-2026-5617) (`docker cp` race conditions), [GO-2026-4887](https://pkg.go.dev/vuln/GO-2026-4887) (AuthZ plugin bypass) and [GO-2026-4883](https://pkg.go.dev/vuln/GO-2026-4883) (plugin-privilege off-by-one) — all reachable only via `init()` chains, with no upstream patch on the v28 line. ([#744](https://github.com/netresearch/ofelia/pull/744), [#746](https://github.com/netresearch/ofelia/pull/746))

## [0.27.0] - 2026-06-25

### Added

- New `[global] job-exec-label-scope` option makes label-defined `job-exec` job naming collision-safe when one central Ofelia daemon watches several independent Compose projects. Since [#300](https://github.com/netresearch/ofelia/issues/300)/[#597](https://github.com/netresearch/ofelia/pull/597) switched the prefix from the container name to the Compose **service** name (to power cross-container `depends-on` references like `database.backup`), two stacks deployed from the same template — e.g. `acme-web` and `globex-web`, both Compose service `web` with an identical `ofelia.job-exec.sync-news` label — silently collapsed to the single key `web.sync-news`: only the first container (running → newest → name order) won and the other stack's job never ran, with no error. This unnoticed regression reopened the exact collision that [#86](https://github.com/netresearch/ofelia/issues/86)/[#114](https://github.com/netresearch/ofelia/pull/114) had closed. The new option selects the prefix scheme: `service` (default — `{service}.{job}`, unchanged, required for cross-container references), `container` (`{container}.{job}` — collision-safe per Docker daemon), or `container-service` (`{container}.{service}.{job}` — descriptive and collision-safe, falling back to `{container}.{job}` for non-Compose containers). It is a daemon-wide INI-only setting (not exposed via container labels, so a single container cannot redefine how every other container's jobs are named) and defaults to `service`, preserving the pre-fix behavior. When switching away from `service`, update cross-container `depends-on`/`on-success`/`on-failure` references to the new scoped names. Independently of the chosen scope, Ofelia now also **logs a loud warning** whenever a job-exec name collision is detected — naming both containers, the dropped job, and the `job-exec-label-scope` remedy — so a collapsed job is never dropped in silence again (the worst part of this bug was the *absence* of any signal). An unrecognized `job-exec-label-scope` value is likewise reported with a warning before falling back to `service`, rather than silently degrading. Closes [#734](https://github.com/netresearch/ofelia/issues/734).

## [0.26.0] - 2026-06-18

### Added

- New bundled webhook presets `healthchecks` and `healthchecks-selfhosted` for [Healthchecks.io](https://healthchecks.io/) and self-hosted instances. The hosted preset pings `https://hc-ping.com/{id}` (where `id` is a UUID or `pingkey/slug`); the self-hosted preset takes the full ping URL via `url = ...`. Both POST a plain-text job/execution summary as the ping body. Because Healthchecks derives up/down state from the URL suffix rather than the body, the docs show the two-webhook pattern (`trigger = success` → base URL, `trigger = error` → `<id>/fail`) for explicit failure signaling. ([#692](https://github.com/netresearch/ofelia/pull/692))

- New `--state-file` / `OFELIA_STATE_FILE` daemon flag persists API-mutated state (jobs created/updated via `POST /api/jobs/create` or `/update` and disable flags from `/disable`) to a JSON file so they survive daemon restarts. Pre-fix, every API-created job lived only in scheduler memory and was lost on `docker compose restart` or pod recycle ([#593](https://github.com/netresearch/ofelia/issues/593)); operators had no way to make UI/API changes durable short of editing the INI by hand. The new flag is opt-in (empty path = disabled, preserving the pre-fix behavior). On startup the file loads after INI/labels and shadows same-named INI/label jobs (API state is authoritative for its own entries). Disable flags apply regardless of origin so an INI-defined job paused from the UI stays paused after restart. Writes are atomic via tmp+rename, file mode is explicitly `0o600`, the on-disk schema carries a `Version` field so future-incompatible changes can be migrated explicitly, and Load enforces a 16 MiB size cap + `DisallowUnknownFields` so a malicious or typo'd file fails closed rather than silently dropping config. Closes [#593](https://github.com/netresearch/ofelia/issues/593).

- `job-exec` jobs can now set the initial pseudo-TTY console size via two new fields: `console-height` (rows) and `console-width` (columns). Useful for jobs that render TUIs, tables, or formatted text — applications that expect a specific terminal geometry (`htop`, `vim`, formatted-output reports) now render correctly. Both default to 0, meaning "use Docker's default size" (preserving pre-fix behavior). Only honored when `tty = true`; otherwise the Docker daemon silently ignores the size. Requires Docker API v1.42+ (Docker Engine 20.10+, released 2020-12) — Ofelia's auto-negotiation handles older daemons gracefully. Closes [#235](https://github.com/netresearch/ofelia/issues/235).

- `job-run` jobs can now customize the signal Ofelia sends to the container's main process at stop time via a new `stop-signal` field, paired with a new `stop-timeout` field that controls the grace period before the Docker daemon escalates to `SIGKILL`. `stop-signal` accepts the canonical name (`SIGINT`, `SIGUSR1`) or the bare suffix (`INT`, `USR1`); empty (the default) preserves the pre-fix behavior of falling back to whatever the container image declared via `STOPSIGNAL` (which itself defaults to `SIGTERM`). `stop-timeout` accepts a Go duration (e.g. `30s`, `2m`); zero (the default) preserves the pre-fix hardcoded 10s grace period and is read only by Ofelia's deadline-cleanup path (`cleanupOnDeadline`) — other shutdown paths inherit the daemon's default. Useful for apps with signal handlers — Node.js workers that handle `SIGINT` cleanly, Java apps that dump threads on `SIGQUIT`, or custom cleanup paths on `SIGUSR1`/`SIGUSR2`, all of which often need more than 10s before `SIGKILL`. Requires Docker API v1.42+ (Docker Engine 20.10+, released 2020-12). Closes [#234](https://github.com/netresearch/ofelia/issues/234).

- New `[docker] startup-retry-count` and `[docker] startup-retry-interval` config (also exposed as `--docker-startup-retry-count` / `OFELIA_DOCKER_STARTUP_RETRY_COUNT` and `--docker-startup-retry-interval` / `OFELIA_DOCKER_STARTUP_RETRY_INTERVAL`) retry the initial Docker connection with exponential backoff before the daemon exits. Default is `startup-retry-count=0`, which preserves the pre-fix "exit on first failure" behavior; setting `startup-retry-count=5 startup-retry-interval=1s` yields a 1s → 2s → 4s → 8s → 16s budget (~31s total). Each per-attempt ping is still bounded by `dockerStartupPingTimeout` (10s) so a wedged daemon cannot inflate startup beyond `(count+1)·10s + Σbackoffs`. The backoff window observes ctx cancellation so SIGTERM during startup drains promptly instead of blocking the full budget (same shape as [#685](https://github.com/netresearch/ofelia/pull/685) / [#687](https://github.com/netresearch/ofelia/pull/687)). Useful for TCP-based Docker hosts where the daemon may briefly be unreachable on startup before health checks settle (socket proxies, remote Docker hosts, Docker-in-Docker). Closes [#523](https://github.com/netresearch/ofelia/issues/523).

- The `save` middleware output permissions are now configurable via two optional octal keys: `save-mode` for the per-execution log/JSON files (default `0600`) and `save-folder-mode` for the save folder (default `0750`). Both accept `0644`, `0o644` or `644`, are capped at `0777` (setuid/setgid/sticky bits rejected), and inherit global→job exactly like `save-folder`. The hardened defaults are unchanged — set a wider mode (e.g. `save-mode = 0644`) to let a non-root operator or shared group read the captured logs on the host. The `0600`/`0750` defaults date to the gosec G301/G306 hardening in `5211e54b`, which made logs readable only by the daemon uid and was a behavioral regression vs. `mcuadros/ofelia` (which wrote `0644`). Like `save-folder`, neither key is in the global Docker-label allow-list, so a scheduled container cannot change the daemon-wide default; `save-folder-mode` applies only when Ofelia creates the folder (`mkdir -p` semantics — an existing bind-mounted folder keeps its current permissions). Closes [#729](https://github.com/netresearch/ofelia/issues/729).

- The bundled `pushover` webhook preset gains an optional `device` field to target one or more named Pushover devices instead of all of a user's devices. Settable via INI (`device = ...`) or Docker label; leaving it unset preserves the prior behavior (delivery to all of the user's devices). Follows the precedent of the generic optional `link` / `link-text` fields rather than a preset-specific hack. ([#715](https://github.com/netresearch/ofelia/pull/715))

### Changed

- **BREAKING (API behavior):** `POST /api/jobs/delete` now returns `403 Forbidden` for jobs that came from INI config or Docker labels, with a message naming the source so operators know which file to edit. Pre-fix this handler silently removed the job from memory until the next reload — a sharp edge that masked surprises ([#593](https://github.com/netresearch/ofelia/issues/593)). Scripts that relied on the pre-fix behavior must switch to `POST /api/jobs/disable` (which now persists across restart) or edit the source config. Jobs created via the web UI / API remain deletable as before.

- `WebhookManager` now caches `*http.Client` per webhook `Timeout` so the underlying transport's keep-alive connection pool survives `cli.Config.rebuildAllMiddlewares` reconciles (which fire after every Docker label change or INI reload). Pre-fix, `NewWebhook` built a fresh `*http.Client` and `*http.Transport` per call inside `GetMiddlewares`, so every reconcile dropped any held keep-alive connections and started fresh TCP/TLS handshakes for every job's webhooks. The cache is keyed by `Timeout` because that's the only per-webhook input that varies (the shared `TransportFactory()` handles TLS/proxy posture and the SSRF allow-list lives in the package-global security config). Webhooks with the same `Timeout` share a client; different timeouts get distinct clients. Standalone `NewWebhook(config, loader)` callers (tests, direct construction) keep building a fresh client per call — the legacy behavior is preserved for non-manager paths. Closes [#674](https://github.com/netresearch/ofelia/issues/674).

### Security

- Extended the `[global] allow-host-jobs-from-labels=false` policy to cover both `job-run` AND `job-service-run` entries that mount host filesystem paths via Docker labels (e.g. `ofelia.job-run.X.volume=/:/host:rw`) **or** inherit a donor container's bind mounts via `volumes-from` (e.g. `ofelia.job-run.X.volumes-from=ofelia` to pull in the daemon's `/var/run/docker.sock` mount). Pre-fix, only `job-local` and `job-compose` were filtered, leaving every container-spawning job type (`job-run` and Swarm `job-service-run`) with open container-to-host privilege-escalation vectors via `volume=` and `volumes-from=`. An attacker controlling labels on any container Ofelia watched could mount `/` into the spawned container directly, or chain via `volumes-from=ofelia` to inherit the Docker socket and gain full daemon access. The new per-job filter:
  - Detects host mounts in `volume` (specs whose source starts with `/`, `.`, or `~`, after whitespace normalization) and drops the entire offending `job-run`.
  - Treats any non-empty `volumes-from` as a violation, because the donor's mounts cannot be inspected at filter time — conservative drop is the only safe call.
  - Fails closed on unexpected param shapes (e.g. a future refactor delivering `[]any` instead of `[]string` cannot silently bypass the policy).
  - Logs a `SECURITY POLICY VIOLATION` per dropped job, naming the job, the vector class (`volume=` vs `volumes-from=`), and the specific specs so operators can triage.
  Named volumes (`my-vol:/data`) and anonymous volumes (`/data` target-only) with no `volumes-from` are unaffected. Closes [#462](https://github.com/netresearch/ofelia/issues/462).

### Fixed

- `PresetLoader.AddLocalPresetDir` now scans the registered directory for `*.yaml` files whose stem collides with a bundled preset name (`slack`, `discord`, `teams`, `matrix`, `ntfy`, `ntfy-token`, `pushover`, `pagerduty`, `gotify`, `json-post`) and emits a startup `slog.Warn` per collision. Pre-fix, `PresetLoader.Load` resolved bundled presets first and never fell through, so a file at `$LOCAL_DIR/json-post.yaml` placed hoping to override the bundled `json-post` was silently ignored at attach time. The warning matches `Load`'s `.yaml`-only resolution path so a `.yml` rename suggestion never misleads operators. The lookup order is documented in `docs/webhooks.md` under "Preset Lookup Order"; inverting the order to prefer local files is deliberately rejected (a local typo shadowing `slack.yaml` would silently break Slack delivery host-wide). Closes [#679](https://github.com/netresearch/ofelia/issues/679).

- `core.middlewareContainer.Use()` now dedups per-instance via an optional `Key() string` interface instead of by `reflect.TypeOf(m).String()`. Pre-fix, two `*middlewares.Webhook` instances handed to the same job collapsed into the first and silently dropped the rest — the failure mode tracked in [#670](https://github.com/netresearch/ofelia/issues/670) and worked around at the webhook layer by PR [#671](https://github.com/netresearch/ofelia/pull/671) with the `WebhookMiddleware` composite. The composite stays in place (no behavior change there), but the structural bug is now closed at the core layer so any future N-instance middleware (e.g. multiple Slack channels, multiple Mail recipient sets) gets correct semantics by construction. Existing 1-per-type middlewares (`Slack`, `Mail`, `Save`, `Overlap`, `WebhookMiddleware`) do not implement `Key()`, so they fall back to the legacy type-string dedup — `j.Use(s.Middlewares()...)` scheduler-to-job propagation still de-duplicates them as before. `*middlewares.Webhook` returns `Config.Name` from `Key()`, so the same-name propagation case (scheduler-level webhook re-propagated to a job that already has it) still de-duplicates correctly. Opt-in via type assertion means downstream consumers with custom middleware implementations need no changes. Closes [#672](https://github.com/netresearch/ofelia/issues/672).

- `[global] default-user = default` now resolves to the container's default user instead of being forwarded verbatim to `docker exec --user default` (which failed with `unable to find user default: no matching entries in passwd file`). The `UserContainerDefault` (`"default"`) sentinel was documented to select the container's default user but `applyDefaultUser` only honored it for a per-job `user`, not when inherited from the global `default-user` — a label-defined job with no explicit `user` took the global value literally. Closes [#716](https://github.com/netresearch/ofelia/issues/716).

- Unknown-key warnings for the `[global]` and `[docker]` sections now suggest the nearest valid key (e.g. `did you mean 'webhook-default-preset'?`), bringing them into parity with the per-job sections. Pre-fix, only job sections offered suggestions; `[global]`/`[docker]` typos just reported `(typo?)`. Closes [#678](https://github.com/netresearch/ofelia/issues/678).

- `RetryExecutor.ExecuteWithRetry` now honors context cancellation during the inter-retry backoff (a `select` on `time.After(delay)` vs. `runCtx.Done()`) instead of a bare `time.Sleep`. Pre-fix, SIGTERM mid-retry blocked daemon shutdown for up to `RetryDelay × MaxRetries` (compounded by exponential backoff); cancellation now drains promptly and returns an error wrapping `context.Canceled`. Sibling to the webhook-backoff fix in [#685](https://github.com/netresearch/ofelia/pull/685) (different code path: job-level retry executor vs. webhook middleware). Closes [#687](https://github.com/netresearch/ofelia/issues/687).

### Removed

- **BREAKING (source-only, pre-1.0):** Removed unused `core/adapters/docker.ClientConfig.HTTPClient` field that was declared in [#681](https://github.com/netresearch/ofelia/pull/681) but never read — a caller setting `cfg.HTTPClient = someClient` silently got the auto-constructed transport instead of theirs. Downstream Go consumers that referenced the field in named struct literals or assignments will see a compile-time error after upgrade (semantically a no-op since the field was already ignored at runtime); permitted under SemVer for the current 0.y.z line (cf. [SemVer §4](https://semver.org/#spec-item-4)). Removing the field turns the silent footgun into a loud compile-time error rather than preserving it as a deprecated no-op. If you need a transport-level injection seam, file a feature request with the use case so the suppression of `disableHTTP2AutoConfig` on caller-supplied transports (the [#668](https://github.com/netresearch/ofelia/issues/668) invariant) can be wired in correctly. ([#693](https://github.com/netresearch/ofelia/pull/693), closes [#684](https://github.com/netresearch/ofelia/issues/684))

### Dependencies

- Go toolchain bumped 1.26.3 → 1.26.4 (`go.mod` and the `make lint`/`make lint-fix` `GOTOOLCHAIN` pins). Routine patch release; `govulncheck` findings for this codebase are unchanged from 1.26.3. Direct deps refreshed in lockstep: `docker/cli` 29.5.2→29.5.3, `go-playground/validator/v10` 10.30.2→10.30.3, and `netresearch/go-cron` 0.14.0→0.15.0. The OpenTelemetry stack was aligned to a single release train (`otel` / `otel/metric` / `otel/trace` / `otel/exporters/otlp/otlptrace/otlptracehttp` 1.43→1.44, `contrib/instrumentation/net/http/otelhttp` 0.68→0.69), plus a full indirect-graph refresh via `go get -u all`. Aligning the otel exporters let `go mod tidy` prune four now-unneeded indirect requires (`google.golang.org/grpc`, `google.golang.org/genproto/googleapis/api`, `…/rpc`, `grpc-ecosystem/grpc-gateway/v2`) that only existed to satisfy the older otel pins. Post-bump, the only two `govulncheck` findings remain the unfixable upstream moby advisories [GO-2026-4887](https://pkg.go.dev/vuln/GO-2026-4887) (AuthZ plugin bypass) and [GO-2026-4883](https://pkg.go.dev/vuln/GO-2026-4883) (plugin-privilege off-by-one) on `docker/docker` v28.5.2, both reachable only via `init()` chains and with no upstream patch yet. ([#720](https://github.com/netresearch/ofelia/pull/720))

## [0.25.1] - 2026-05-16

### Added

- **Upgrade impact:** webhooks configured with `url = ...` but no `preset` were previously rejected at startup (the documented "Custom Webhooks" section promised they would work but the code returned `preset specification cannot be empty`). They now attach and fire using the new bundled `json-post` JSON-POST preset. Audit existing `[webhook "..."]` sections and `webhooks:` job references before upgrading — a stale URL-only config that previously sat inert will now actually send a JSON POST to whatever's in `url`. Pin `webhook-allowed-hosts` to a specific list if you want to restrict egress; set `webhook-default-preset =` (empty) to preserve pre-upgrade behavior and require every webhook to declare `preset` explicitly.

  New bundled `json-post` webhook preset and a `[global] webhook-default-preset` selector that ships with `json-post` as the default fallback. A webhook configured with just a `url = ...` (or Docker label `ofelia.webhook.<name>.url: ...`) now works out of the box — Ofelia POSTs a JSON payload describing the job and execution to that URL, no custom preset YAML required. The fallback preset is selected via `EffectiveDefaultPreset()` at attach time, so live INI / label changes to `webhook-default-preset` take effect on the next attach without restart. Three-state semantics are unambiguous: the field is `*string`, so nil = "operator did not set" (use bundled fallback), non-nil empty = "explicit opt-out", non-nil non-empty = "operator's chosen fallback name". When opt-out is in effect, the attachment-failed error message names `webhook-default-preset` explicitly so operators can grep their way from logs to the docs. Fixes [#676](https://github.com/netresearch/ofelia/issues/676).

### Fixed

- Webhook retry backoff and in-flight HTTP requests now honor ctx cancellation, so `SIGTERM` on a daemon mid-retry drains promptly instead of pinning a goroutine for up to `retry-delay × retry-count` (plus one `timeout` for the in-flight request). On defaults this caps shutdown contribution at `5s × 3 + 30s ≈ 45s`; operators with `retry-delay = 60s` / `retry-count = 5` previously saw multi-minute shutdown stalls. `(*Webhook).sendWithRetry` replaces the bare `time.Sleep` with a `select` over `(time.After, ctx.Done)`; `(*Webhook).send` derives `reqCtx` from `ctx.RunContext()` rather than `context.Background()` so an in-flight HTTP write is also cut on cancellation. On cancel, callers see an error chain wrapping `context.Canceled` / `context.DeadlineExceeded` (was previously `"all N attempts failed, last error: ..."` after the full timeout). ([#685](https://github.com/netresearch/ofelia/pull/685), fixes [#673](https://github.com/netresearch/ofelia/issues/673))

- `DOCKER_HOST=http://...` now works for hijack-using APIs (`ContainerExecAttach` / `run_exec`, `ContainerAttach`, `ContainerLogs --follow`). The Docker SDK's hijack-path dialer falls through to `net.Dial(cli.proto, cli.addr)`; for `proto == "http"` that was `net.Dial("http", addr)`, which Go's `net` package rejects with `unknown network http` (only `"tcp"`, `"unix"`, etc. are valid network names). Container discovery was unaffected because the regular HTTP path uses `http.Client.Do`, not the SDK's hijack dialer. Fix installs an explicit TCP `DialContext` on the `http://` transport (new `applyHTTPTransport`) so the SDK picks our dialer via `dialerFromTransport` and never reaches the broken fallback. Sibling fix to [#681](https://github.com/netresearch/ofelia/pull/681) (same SDK dialer, different failure mode). ([#682](https://github.com/netresearch/ofelia/issues/682))

- `DOCKER_HOST=tcp://...` (plain HTTP, no TLS) — typically routed through a socket proxy such as `tecnativa/docker-socket-proxy` — now works for hijack-using APIs: `ContainerExecAttach` (the `run_exec` job type), `ContainerAttach`, and `ContainerLogs --follow`. In v0.25.0 the first plain-HTTP request through the transport triggered Go's lazy HTTP/2 auto-config, which allocated `*http.Transport.TLSClientConfig` in place (to seed `NextProtos=[h2 http/1.1]` for ALPN). The Docker SDK's hijack dialer reads `baseTransport.TLSClientConfig` as its "TLS is required" signal and then dialed TLS against a plaintext daemon, surfacing as `cannot connect to the Docker daemon. Is 'docker daemon' running on this host?: tls: first record does not look like a TLS handshake`. Container discovery (the non-hijack HTTP path) was unaffected. Fix sets `*http.Transport.TLSNextProto` to a non-nil empty map on the non-TLS apply paths (`applyUnixTransport`, `applyTCPTransport`, `applyPlainTransport`) — the documented stdlib opt-out for HTTP/2 auto-config. TLS Docker hosts (`https://`, `tcp+tls://`) leave `TLSNextProto` nil so ALPN h2 negotiation remains intact. ([#681](https://github.com/netresearch/ofelia/pull/681), fixes [#668](https://github.com/netresearch/ofelia/issues/668))

- Jobs that reference more than one webhook now fire every listed webhook, including any globally-configured ones. `core.middlewareContainer.Use()` deduplicates by reflect type, so handing it two `*middlewares.Webhook` instances kept only the first one and silently dropped the rest — leaving the second webhook (typically the error-trigger one) attached to nothing and never invoked. The same dedup also shadowed the scheduler-level `*middlewares.WebhookMiddleware` against per-job webhooks during scheduler→job middleware propagation, so any job that declared `webhooks:` silently lost the global notifications too. The fix attaches a single per-job composite (`middlewares.NewWebhookMiddleware`) that carries the union of `[global] webhook-webhooks` and the job's own `webhooks` selector, deduplicated by name. The previously-silent `wm.GetMiddlewares` error path (unknown webhook name, preset-load failure, missing required variable) now emits a `slog.Error` keyed by job name and webhook list, so misconfigured webhooks are visible in the log. To verify after upgrade: register two webhooks with different `trigger` values on a job that fails (e.g. `wh-success` with `trigger: success`, `wh-error` with `trigger: error`); only the error webhook should receive a payload on failure, but both must be wired and the success one must fire on success. ([#670](https://github.com/netresearch/ofelia/issues/670))

## [0.25.0] - 2026-05-14

### Added

- `tcp+tls://` is back on the `DOCKER_HOST` allow-list now that the TLS plumbing from [#613](https://github.com/netresearch/ofelia/pull/613) wires `DOCKER_CERT_PATH` / `DOCKER_TLS_VERIFY` (and the equivalent `ClientConfig.TLSCertPath` / `TLSVerify` overrides) into the custom HTTP transport. PR [#612](https://github.com/netresearch/ofelia/pull/612) had withheld it to avoid a silent plain-TCP downgrade; that risk is now closed by the existing `TestCreateHTTPClient_TCPPlusTLSEnablesTLS` regression test plus the new `TestNewClientWithConfig_TCPPlusTLSScheme` allow-list assertion. ([#625](https://github.com/netresearch/ofelia/pull/625), fixes [#616](https://github.com/netresearch/ofelia/issues/616))

### Changed

- **BREAKING:** `DOCKER_HOST` scheme is now validated against an allow-list (`unix://`, `tcp://`, `tcp+tls://`, `http://`, `https://`, `npipe://`) and normalized to lowercase. Unsupported schemes (`ssh://`, `fd://`, bogus values) now fail at startup with a clear error instead of silently falling through to a plain-TCP transport. Fixes case-sensitivity bug for `TCP://` / `UNIX://`. Configurations that previously relied on the silent fallthrough will now fail loudly — operators using `ssh://` should switch to an SSH-forwarded socket and point `DOCKER_HOST` at the forwarded `unix://` path. ([#612](https://github.com/netresearch/ofelia/pull/612), fixes [#609](https://github.com/netresearch/ofelia/issues/609))
- Webhook global config now lives at a single source of truth: `c.WebhookConfigs.Global` aliases `&c.Global.WebhookGlobalConfig`, eliminating the dual-store antipattern that PR [#618](https://github.com/netresearch/ofelia/pull/618) papered over with a hand-rolled `syncGlobalWebhookConfig` copy. Every entry point that mutates the embedded struct from the INI side (initial INI parse, INI live-reload) is automatically visible to `WebhookManager` without an explicit sync call. INI live-reload of `webhook-allowed-hosts` now also re-runs `WebhookManager.InitManager()` so the URL validator picks up the new whitelist at runtime — previously the data store was refreshed but the security validator stayed snapshotted at startup, so tightening the whitelist via live-reload had no enforcement effect until restart. The Docker label sync path still parses into a scratch Config and merges back via `mergeWebhookConfigs`/`syncWebhookConfigs`, which only forward the `webhook-webhooks` selector and per-webhook definitions — see follow-up. ([#620](https://github.com/netresearch/ofelia/issues/620))
- `PresetLoader` now caches a single `*http.Client` (constructed in `NewPresetLoader` from `TransportFactory()`) instead of building a fresh client and `*http.Transport` on every `loadFromURL` call. Bursty preset fetches now share the underlying connection pool / idle-conn reuse. Test ordering constraint: tests overriding the transport factory via `SetTransportFactoryForTest` MUST install the override BEFORE calling `NewPresetLoader` — replacing the factory afterwards has no effect on the cached client. The deprecated Slack middleware (`middlewares/slack.go`) also routes its fallback `*http.Client` through `TransportFactory()` so notifications inherit the webhook stack's TLS / proxy posture instead of `http.DefaultTransport`; defense-in-depth for the deprecation window. ([#630](https://github.com/netresearch/ofelia/issues/630))

### Deprecated

- Docker label key `ofelia.webhooks` is renamed to `ofelia.webhook-webhooks` to match the documented INI `[global]` key name. A user copying their INI `webhook-webhooks` value verbatim into Docker labels previously hit an "Unknown global label keys" warning and silently lost the value. The legacy `ofelia.webhooks` form still works for backward compatibility but logs a one-shot deprecation warning per process — migrate to `ofelia.webhook-webhooks` before the next major release. The other unprefixed legacy forms (`ofelia.allow-remote-presets`, `ofelia.trusted-preset-sources`, `ofelia.preset-cache-ttl`, `ofelia.preset-cache-dir`) were never accepted from labels because their canonical forms remain INI-only for SSRF reasons (see [#486](https://github.com/netresearch/ofelia/issues/486)). ([#620](https://github.com/netresearch/ofelia/issues/620))

  **Before:**
  ```yaml
  labels:
    ofelia.webhooks: "slack-alerts"  # legacy — emits one-shot deprecation warning
  ```

  **After:**
  ```yaml
  labels:
    ofelia.webhook-webhooks: "slack-alerts"  # canonical — matches INI [global]
  ```

### Security

- Go toolchain bumped from 1.26.2 to 1.26.3, clearing six standard-library advisories that `govulncheck` reaches from this codebase: [GO-2026-4986](https://pkg.go.dev/vuln/GO-2026-4986) and [GO-2026-4977](https://pkg.go.dev/vuln/GO-2026-4977) (`net/mail`), [GO-2026-4982](https://pkg.go.dev/vuln/GO-2026-4982) and [GO-2026-4980](https://pkg.go.dev/vuln/GO-2026-4980) (`html/template`), [GO-2026-4971](https://pkg.go.dev/vuln/GO-2026-4971) (`net`), [GO-2026-4918](https://pkg.go.dev/vuln/GO-2026-4918) (`net/http`). Direct deps refreshed in lockstep: `docker/cli` 29.4.2→29.4.3, `golang.org/x/crypto` 0.50→0.51, `golang.org/x/term` 0.42→0.43, `golang.org/x/text` 0.36→0.37, plus a full indirect-graph refresh via `go get -u all`. Post-bump, only two `govulncheck` findings remain — the unfixable upstream moby advisories [GO-2026-4887](https://pkg.go.dev/vuln/GO-2026-4887) (AuthZ plugin bypass) and [GO-2026-4883](https://pkg.go.dev/vuln/GO-2026-4883) (plugin-privilege off-by-one) on `docker/docker` v28.5.2, both reachable only via `init()` chains and with no upstream patch yet. Includes a small test-only fix to `TestSDKDockerProviderWaitContainerContextCanceled` to de-race the mock so go1.26.3's scheduler timing doesn't surface the preexisting `select` race between `<-ctx.Done()` and `<-respCh` (closed). ([#662](https://github.com/netresearch/ofelia/pull/662))
- Docker SDK adapter now fails closed when `DOCKER_HOST=https://...` is set with TLS material configured (`DOCKER_CERT_PATH` env or `ClientConfig.TLSCertPath`) but the cert material at the configured path is unreadable or invalid (typo, missing files, broken volume mount, secrets not yet populated). Previously `applyDockerTLS` emitted a `slog.Warn` and left `TLSClientConfig` nil; the SDK then dialed with Go's default TLS — system CA pool, **no** client cert — silently downgrading the operator's declared mTLS into an unauthenticated TLS handshake. The new typed sentinel `ErrHTTPSRequiresUsableCertMaterial` makes the misconfiguration loud at startup. Asymmetry vs `tcp+tls://`: `https://` *without* any TLS material configured remains fail-open (operator legitimately uses the system CA bundle); only the misconfigured-material case fails. The warn-and-continue in `applyDockerTLS` remains as defense-in-depth for direct `createHTTPClient` callers (tests). ([#653](https://github.com/netresearch/ofelia/issues/653), follow-up to [#646](https://github.com/netresearch/ofelia/pull/646))
- SMTP middleware (`middlewares/mail.go`) now defaults to `MandatoryStartTLS` instead of inheriting go-mail's `OpportunisticStartTLS`. The previous default silently sent SMTP credentials and message body in cleartext when the server did not advertise STARTTLS — even when `smtp-tls-skip-verify = false` — violating the operator's intent. New `smtp-tls-policy` INI key accepts `mandatory` (default), `opportunistic`, or `none`; unknown values are normalized to `mandatory` and a `WARN`-level log line is emitted (defensive enum handling: a typo cannot weaken transport security). **BREAKING:** operators whose SMTP servers do not advertise STARTTLS (legacy local relays, MailHog dev fixtures) will see send failures after upgrade — set `smtp-tls-policy = opportunistic` (trusted-loopback paths) or `smtp-tls-policy = none` (test fixtures only) to restore the previous behavior. See `docs/TROUBLESHOOTING.md` for migration recipes. ([#653](https://github.com/netresearch/ofelia/issues/653))
- Webhook URL allow-list now emits a single startup-time `WARN` when the resolved `webhook-allowed-hosts` admits all hosts (empty / unset / contains `*`). Previously a typo in the INI key collapsed silently into the `["*"]` default, yielding wide-open egress with no operator-visible signal that the allow-list they thought they had configured was actually empty. The warning fires once from `SetGlobalSecurityConfig` (the documented startup seam called from `NewWebhookManager`) — not per request — and includes a hint at the corrective INI key plus a link to the issue. No behavior change for operators who intentionally run wide-open egress; the warning is recoverable noise that documents the security posture in the log. ([#653](https://github.com/netresearch/ofelia/issues/653))
- Remote preset fetches (`webhook-allow-remote-presets = true`) now route through the same `TransportFactory()` used by the webhook stack instead of `http.DefaultClient`. The previous code relied implicitly on Go stdlib defaults for TLS verification — safe today, but untested and easy to regress if a future change mutates `http.DefaultTransport`. The TLS posture is now explicit, centrally configurable alongside webhook delivery, and pinned by regression tests (self-signed cert rejection + `InsecureSkipVerify` posture check). No behavior change for operators on default config. ([#615](https://github.com/netresearch/ofelia/issues/615))
- Docker SDK adapter now honors `DOCKER_TLS_VERIFY` and `DOCKER_CERT_PATH` for HTTPS / `tcp+tls` hosts. The custom HTTP client previously replaced the SDK's `FromEnv`-configured TLS transport wholesale, silently discarding the client cert and pinned CA. Connections to mTLS-protected Docker daemons proceeded without a client cert and against the system CA pool — operators believing they had mTLS were getting unauthenticated connections. New `ClientConfig.TLSCertPath` / `TLSVerify` fields allow explicit override with config > env precedence. **Upgrade impact:** if your `https://` Docker daemon previously accepted Ofelia connections without verifying client certs, upgrading will cause the dial to fail until valid `ca.pem` / `cert.pem` / `key.pem` exist at `DOCKER_CERT_PATH`. ([#613](https://github.com/netresearch/ofelia/pull/613), fixes [#607](https://github.com/netresearch/ofelia/issues/607))
- Docker SDK adapter now fails closed when `DOCKER_HOST=tcp+tls://...` is set without TLS material (`DOCKER_CERT_PATH` / `DOCKER_TLS_VERIFY` env vars or `ClientConfig.TLSCertPath` / `TLSVerify` overrides). Previously `resolveTLSConfig` returned `(nil, nil)` and the SDK dialed TLS using Go's stdlib defaults — system CA bundle, **no** client cert — silently downgrading the operator's declared mTLS into an unauthenticated TLS handshake against any daemon that did not strictly require client auth. `tcp+tls://` is an *explicit* TLS opt-in (unlike the ambiguous `tcp://`), so the new typed sentinel `ErrTCPTLSRequiresCertMaterial` makes the misconfiguration loud at startup rather than silent at runtime. `tcp://` and `https://` remain fail-open. **Upgrade impact:** if you set `DOCKER_HOST=tcp+tls://...` without configuring TLS material, Ofelia will now refuse to start — set `DOCKER_CERT_PATH` (and optionally `DOCKER_TLS_VERIFY`) to a directory containing readable `ca.pem` / `cert.pem` / `key.pem`, or switch to `https://` if you genuinely want fail-open-with-warning. ([#627](https://github.com/netresearch/ofelia/issues/627), surfaced during review of [#625](https://github.com/netresearch/ofelia/pull/625))

### Fixed

- `MaxRuntime` cancellation now stops *and removes* the container or swarm service that was running, instead of leaving an orphaned process behind. [#651](https://github.com/netresearch/ofelia/pull/651) wired a wrapper-level deadline so the inner SDK calls returned when the deadline fired, but the deferred `deleteContainer` reused the already-cancelled parent context — so the stop/remove API calls were rejected before they reached the daemon. The cleanup path now uses a fresh `context.WithTimeout(context.Background(), jobCleanupTimeout)` (`jobCleanupTimeout = 30s`) so stop/remove still runs after the parent deadline fires, and the same fix applies to `RunServiceJob`'s service teardown. Operators previously seeing `Exited` containers piling up after every MaxRuntime-bounded job should see them properly removed after this release. ([#659](https://github.com/netresearch/ofelia/pull/659), fixes [#655](https://github.com/netresearch/ofelia/issues/655), follow-up to [#651](https://github.com/netresearch/ofelia/pull/651) / [#638](https://github.com/netresearch/ofelia/issues/638))
- Docker label `[global]` keys outside the webhook subsystem now actually reach the live `c.Global` instead of being silently dropped. Setting e.g. `ofelia.smtp-host=mail.example.com` on a service container previously decoded into a scratch `Config.Global` that `mergeJobsFromDockerContainers` (boot) and `dockerContainersUpdate` (reconcile) discarded after the per-job / webhook merge — `mergeMailDefaults` then inherited the unchanged INI defaults so jobs saw empty SMTP with no warning. New per-subsystem helpers (`mergeSlackGlobals`, `mergeMailGlobals`, `mergeSaveGlobals`, `mergeSchedulingGlobals`) plus the `applyAllowListedGlobals` aggregator wire every allow-listed non-webhook global (Slack, Mail, Save, `log-level`, `max-runtime`, `notification-cooldown`, `enable-strict-validation`) through both call sites with the same "INI value wins when set; label only fills empty/default" precedence as `mergeWebhookGlobals` from [#650](https://github.com/netresearch/ofelia/pull/650). Plain-bool fields (`SMTPTLSSkipVerify`, `EnableStrictValidation`) keep `mergeMailDefaults`' documented asymmetric policy: a label may UPGRADE false→true but cannot downgrade. Runtime label changes to `log-level` and `notification-cooldown` now also re-apply the process-wide knobs (`ApplyLogLevel`, `initNotificationDedup`) without a daemon restart. **Documented limitation** (matches existing INI live-reload behavior, out of scope for this fix): jobs whose own labels did not change in a reconcile pass keep their previously-inherited per-job middleware values until the next per-job change or restart — `mergeNotificationDefaults` only fills empty fields, so once inherited from the previous global, the per-job copy will not re-inherit. ([#652](https://github.com/netresearch/ofelia/issues/652), sibling fix to [#650](https://github.com/netresearch/ofelia/pull/650))
- `DOCKER_HOST=tcp://...` combined with `DOCKER_CERT_PATH` (or the equivalent `ClientConfig.TLSCertPath` override) now actually negotiates TLS end-to-end. Previously the custom HTTP transport was wired with TLS material via [#613](https://github.com/netresearch/ofelia/pull/613), but the SDK kept the `tcp://` URL and Go's `http.Transport` only triggers TLS for `https://` URLs — so the cert material was silently unused and connections went out plaintext. `NewClientWithConfig` now mirrors the docker CLI's silent `tcp://` -> `https://` upgrade when TLS material is present, so the SDK and transport agree on the scheme and the configured certificate / pinned CA actually applies on the wire. Operators previously relying on `DOCKER_HOST=tcp://...` plus TLS env vars to reach an mTLS-protected daemon will now succeed instead of silently downgrading; operators who want plain TCP must omit `DOCKER_CERT_PATH` (or use explicit `tcp+tls://` for TLS). ([#634](https://github.com/netresearch/ofelia/issues/634), follow-up to [#613](https://github.com/netresearch/ofelia/pull/613))
- Docker API version negotiation at startup is now bounded by a configurable `NegotiateTimeout` (default 30s). Previously `NewClientWithConfig` called `NegotiateAPIVersion` with `context.Background()`, so a reachable-but-wedged Docker daemon (e.g. a socket proxy with a hung upstream) could hang Ofelia at startup with no diagnostic output. The deadline-exceeded path now logs a warning so operators can correlate startup slowness with daemon health ([#611](https://github.com/netresearch/ofelia/pull/611), fixes [#608](https://github.com/netresearch/ofelia/issues/608))
- Remaining unbounded Docker SDK calls are now wrapped in `context.WithTimeout`, so a reachable-but-wedged daemon can no longer stall the periodic `/health` and `/ready` checker (5s per call), the daemon startup sanity Pings in `NewDockerHandler` and `buildSDKProvider` (10s each, derived from the handler's own context so SIGINT during startup also cancels), or the `ofelia doctor` diagnostic (5s per Ping and per `HasImageLocally` call — per-call rather than overall to avoid falsely failing slow daemons with many images). `web/health.go` was the most visible regression because monitoring agents would never observe a non-2xx response when the daemon wedged. The three timeout values are unexported constants — see code comments for rationale; file an issue if your environment needs different bounds. Also adds `SDKDockerProviderConfig.NegotiateTimeout` to plumb the test-friendly negotiation bound from [#611](https://github.com/netresearch/ofelia/pull/611) one layer up. ([#636](https://github.com/netresearch/ofelia/pull/636), fixes [#614](https://github.com/netresearch/ofelia/issues/614))
- `[global]` section now recognizes the documented `webhook-*` keys (`webhook-allow-remote-presets`, `webhook-preset-cache-ttl`, `webhook-trusted-preset-sources`, `webhook-preset-cache-dir`, `webhook-allowed-hosts`, `webhook-webhooks`) without emitting "Unknown configuration key" warnings, and the values are now applied to the webhook subsystem. Live-reload also re-syncs into `WebhookConfigs.Global` so runtime edits to `webhook-allowed-hosts` take effect without a restart. **Upgrade note:** if you previously used the unprefixed forms (`allow-remote-presets`, `webhooks`, `preset-cache-ttl`, etc.) under `[global]` — they were never documented but were tolerated by the old hand-rolled parser — rename them to the documented `webhook-*` form. The old keys now produce "Unknown configuration key" warnings and the values silently fall back to defaults. ([#618](https://github.com/netresearch/ofelia/pull/618), fixes [#604](https://github.com/netresearch/ofelia/issues/604))
- `DOCKER_HOST=tcp://...` now correctly drives the HTTP transport's dialer when `ClientConfig.Host` is empty. Previously the dialer was hard-pinned to `unix:///var/run/docker.sock` while the SDK was directed at the env-supplied TCP host, so every request silently routed to a non-existent unix socket and surfaced as a misleading "Cannot connect to the Docker daemon at tcp://..." error. Most commonly hit with Docker socket proxies (e.g. tecnativa/docker-socket-proxy). The actual code change cascaded in via [#613](https://github.com/netresearch/ofelia/pull/613); this entry documents the original report and adds the troubleshooting recipe. ([#606](https://github.com/netresearch/ofelia/pull/606), fixes [#605](https://github.com/netresearch/ofelia/issues/605))
- `ExecServiceAdapter.Create` and `.Run` no longer panic on a nil `ExecConfig` or on nil `stdout`/`stderr` writers in non-TTY mode. Both paths now return typed sentinel errors (`ErrNilExecConfig`, `ErrNoExecOutputWriter`) that callers can branch on via `errors.Is`. Previously the SDK would dereference the nil config (`config.User`, etc.) or `stdcopy.StdCopy` would panic on `(nil, nil)` writers when there was output to demultiplex. ([#619](https://github.com/netresearch/ofelia/pull/619), refs [#610](https://github.com/netresearch/ofelia/issues/610))
- Defense-in-depth: every public method on every `*ServiceAdapter` in `core/adapters/docker/` (`Container`, `Exec`, `Image`, `Event`, `Network`, `Swarm`, `System`) now returns the new sentinel `ErrNilDockerClient` instead of panicking with a nil-pointer dereference if the embedded SDK client is nil. The `newClientFromSDK` constructor always wires a non-nil client, so this is only reachable through hand-rolled adapter values (test fixtures or wiring bugs) — but the guards convert what would otherwise be a panic in a hot goroutine into a branchable, actionable failure. `Subscribe` and `Wait` (channel-returning) push the sentinel to `errCh` and close both channels synchronously without launching a goroutine. ([#639](https://github.com/netresearch/ofelia/pull/639), fixes [#623](https://github.com/netresearch/ofelia/issues/623))
- The Docker label sync path now mirrors the `WebhookConfigs.Global → &Global.WebhookGlobalConfig` pointer alias that `NewConfig` set up for the live config in [#637](https://github.com/netresearch/ofelia/pull/637) / [#620](https://github.com/netresearch/ofelia/issues/620). The scratch `Config` built by `dockerContainersUpdate` and `mergeJobsFromDockerContainers` previously used a struct literal with `WebhookConfigs: NewWebhookConfigs()`, which left `parsed.WebhookConfigs.Global` pointing at a fresh `*WebhookGlobalConfig` disjoint from `parsed.Global.WebhookGlobalConfig`. Any future `mergeWebhookConfigs` field that reads from the parsed `WebhookConfigs.Global` (notably the `PresetCacheTTL` forwarding planned in [#640](https://github.com/netresearch/ofelia/pull/640)) would silently observe the 24h default instead of the just-decoded label value. Both call sites now go through a `newScratchConfig(c)` helper that re-establishes the alias. ([#641](https://github.com/netresearch/ofelia/issues/641))
- Symmetric nil-guards on the *From* helpers in the Docker adapter: `convertFromSwarmService(nil)` returns nil, `convertTaskTemplateFromSwarm` is a no-op when either side is nil, `convertFromSwarmTask(nil)` returns the zero `domain.Task`, and `convertFromSDKEvent(nil)` returns the zero `domain.Event`. `ContainerServiceAdapter.Create` now returns the new typed sentinel `ErrNilContainerConfig` (errors.Is-branchable, mirrors `ErrNilExecConfig` from [#619](https://github.com/netresearch/ofelia/pull/619)) instead of dereferencing `config.HostConfig` / `config.NetworkConfig` on a nil config. `convertFromSwarmTask`, `convertFromSDKEvent`, and `Container.Create`'s `HostConfig` deref are reachable through public API paths (the event-consumer goroutine, `WaitForServiceTasks`, the executor); `convertFromSwarmService` and `convertTaskTemplateFromSwarm` were latent (test-callable only) but guarded for symmetry with PR [#626](https://github.com/netresearch/ofelia/pull/626). Same bug class as [#619](https://github.com/netresearch/ofelia/pull/619) and [#626](https://github.com/netresearch/ofelia/pull/626). The issue's mention of "nil `e.Actor`" was technically inaccurate — `events.Actor` is a value type, not a pointer, so only the outer `*events.Message` can be nil. ([#632](https://github.com/netresearch/ofelia/issues/632), refs [#626](https://github.com/netresearch/ofelia/pull/626) / [#622](https://github.com/netresearch/ofelia/issues/622))
- Docker adapter convert helpers (`convertToSwarmSpec`, `convertTaskTemplateToSwarm`, `convertToMount`) now nil-guard their pointer arguments and return a zero value instead of panicking. Same bug class as #619 — the helpers dereferenced `spec`, `src`, and `m` without a nil-check. Production callers always pass a non-nil pointer today (so this is defense-in-depth, not a wild-fire fix), but the helper signatures invited unsafe direct calls from tests and refactors. ([#626](https://github.com/netresearch/ofelia/pull/626), fixes [#622](https://github.com/netresearch/ofelia/issues/622))
- Sibling-hunt completion of the `convert.go` family of nil-guard gaps: `convertFromAPIContainer(nil)` and `convertFromNetworkResource(nil)` return the zero `domain.Container` / `domain.Network`, `convertFromNetworkInspect(nil)` returns nil (mirroring `convertFromSwarmService` from [#648](https://github.com/netresearch/ofelia/pull/648)), and `convertToMount(nil)` returns the zero `mount.Mount` — closing the asymmetry where every other `convertTo*` helper in `container.go` (`convertToHostConfig`, `convertToNetworkingConfig`, `convertToEndpointSettings`, `convertToContainerConfig`) already nil-guarded its argument and only `convertToMount` did not. All four are reached via `&loopVar` from a `range` over a slice in production, so no live panic exists today; this is defense-in-depth for unsafe signature contracts. Same bug class as [#619](https://github.com/netresearch/ofelia/pull/619) / [#626](https://github.com/netresearch/ofelia/pull/626) / [#632](https://github.com/netresearch/ofelia/issues/632), completing what [#648](https://github.com/netresearch/ofelia/pull/648) started. ([#654](https://github.com/netresearch/ofelia/issues/654), refs [#648](https://github.com/netresearch/ofelia/pull/648) / [#626](https://github.com/netresearch/ofelia/pull/626))

### Tests

- Stabilize `TestHealthStatus` race against the `NewHealthChecker` background goroutine — build the `HealthChecker` directly in the test so the auto-injected `docker=Unhealthy` check cannot leak into the aggregated status before `GetHealth()` runs. ([#606](https://github.com/netresearch/ofelia/pull/606))
- New `TestConfigGlobalKeysAreDocumented` walks the embedded middleware structs in `Config.Global` via reflection and asserts each `mapstructure` key is mentioned in at least one operator-facing docs file (`docs/CONFIGURATION.md`, `docs/webhooks.md`, `docs/QUICK_REFERENCE.md`, `docs/TROUBLESHOOTING.md`, `README.md`). Catches the same drift class as #604 / #621 mechanically. ([#621](https://github.com/netresearch/ofelia/issues/621))
- Per-handler unit tests for the Docker scheme dispatch table (`TestSchemeHandlers_ApplyDirect`) invoke each `apply*` function directly with a fresh `*http.Transport` and assert the per-scheme `ForceAttemptHTTP2` / `DialContext` shape. Catches a refactor that breaks one scheme without breaking the others — previously the `apply*` functions were only covered transitively. New `TestCreateHTTPClient_UnknownSchemeFallback` pins the defensive plain-HTTP/1.1 fallback for unrecognized schemes (production rejects upstream via `NewClientWithConfig`; this exercises the seam, not the production gate). Tightened `TestNewClientWithConfig_ReadsDOCKERHOSTOnce` `env_only` branch from `<= 1` to `== 1` so a regression that drops the env read entirely is caught. ([#633](https://github.com/netresearch/ofelia/issues/633), follow-up to [#629](https://github.com/netresearch/ofelia/pull/629))

### Documentation

- Reconcile the `tcp://` Docker host scheme docs with reality: the godoc in `core/adapters/docker/client.go` (the `schemeHandlers` table) and the scheme table in `docs/TROUBLESHOOTING.md` previously claimed `tcp://` "auto-upgrades to TLS when `DOCKER_TLS_VERIFY` / `DOCKER_CERT_PATH` are set", mirroring the docker CLI. The transport-layer half of that upgrade was wired in [#613](https://github.com/netresearch/ofelia/pull/613), but Go's `http.Transport` only performs TLS on `https://` URLs — so the `TLSClientConfig` was loaded with cert material that the SDK never offered on the wire. Operators following the docker CLI mental model believed they had mTLS while their connections went out as plain TCP. The fix removes the `applyDockerTLS` call from `applyTCPTransport` (silent ineffective wiring is worse than failing loud), updates the godoc and the troubleshooting table to point operators at `tcp+tls://` ([#616](https://github.com/netresearch/ofelia/issues/616)) or `https://` for TLS over TCP, and replaces the misleading `TestCreateHTTPClient_TCPWithTLSEnvUpgrades` regression test with `TestCreateHTTPClient_TCPDoesNotWireTLSEvenWithEnv` to pin the contract going forward. The deeper docker-CLI parity story — automatic `tcp://` to `https://` URL rewriting at the SDK layer — is tracked separately in [#634](https://github.com/netresearch/ofelia/issues/634) and intentionally not addressed here. ([#628](https://github.com/netresearch/ofelia/issues/628))
- Reconcile **Slack** middleware key documentation with the actual struct fields in `middlewares.SlackConfig`. Removed the documented-but-rejected `slack-url` (typo for `slack-webhook`), `slack-channel`, `slack-mentions`, `slack-icon-emoji`, and `slack-username` keys from `docs/CONFIGURATION.md` and `docs/QUICK_REFERENCE.md`. The legacy Slack middleware (deprecated, scheduled for removal in v1.0.0) only accepts `slack-webhook` and `slack-only-on-error`; for channel routing, mentions, custom username/avatar, etc., migrate to a `[webhook "name"]` section with `preset = slack` ([webhook docs](docs/webhooks.md)). ([#621](https://github.com/netresearch/ofelia/issues/621))
- Reconcile **Save** middleware key documentation with the actual struct fields in `middlewares.SaveConfig`. Documented the existing `restore-history` and `restore-history-max-age` global keys (supported by the parser but undocumented) and removed the unimplemented `save-format` and `save-retention` keys from `docs/CONFIGURATION.md`. ([#621](https://github.com/netresearch/ofelia/issues/621))
- Document the previously-undocumented (or only partially-documented) `[global]` keys surfaced by the docs-vs-code drift sweep: `notification-cooldown` (notification deduplication window) and `smtp-tls-skip-verify` (with a dedicated security trade-off section in `docs/TROUBLESHOOTING.md` covering when it's acceptable, when it's not, and recommended alternatives) in `docs/CONFIGURATION.md`; and the webhook globals `webhook-webhooks`, `webhook-trusted-preset-sources`, `webhook-preset-cache-dir`, plus an explicit INI-vs-Docker-labels callout (only `webhook-webhooks` is exposed via labels; the SSRF-sensitive globals are INI-only) in `docs/webhooks.md`. ([#635](https://github.com/netresearch/ofelia/issues/635), refs [#621](https://github.com/netresearch/ofelia/issues/621), [#604](https://github.com/netresearch/ofelia/issues/604))
- Reconcile the README "Features" bullet and `OFELIA_POLL_INTERVAL` env-var row with the post-[#586](https://github.com/netresearch/ofelia/pull/586) split-poll-interval contract: container detection now defaults to Docker events (`--docker-events=true` is the only CLI flag in this group). The other three knobs are INI-only — `docker-poll-interval` (opt-in container polling), `polling-fallback` (default `10s`, auto-engages polling if the event stream fails), and `config-poll-interval` (default `10s`, drives INI file reloads). The legacy `--docker-poll-interval` CLI flag stays for backward compatibility but now sets the deprecated unified `Config.Docker.PollInterval`, which `ApplyDeprecationMigrations` splits into the new INI keys at parse time. The `docs/ARCHITECTURE_DIAGRAMS.md` polling-defaults diagram is swapped to match — Events are default-on, Poll is opt-in, polling-fallback is shown as the third edge. Also documents the previously-undocumented `web-trusted-proxies` global key in `docs/CONFIGURATION.md` with a security note covering the X-Forwarded-For spoofing risk if the CIDR list is too permissive (sibling-hunt finding from PR [#645](https://github.com/netresearch/ofelia/pull/645)). The `TestConfigGlobalKeysAreDocumented` drift detector is extended to walk direct (non-anonymous) `Config.Global` fields too, closing the `TODO(#635)` so future undocumented direct keys (not just embedded middleware config keys) are caught mechanically. ([#656](https://github.com/netresearch/ofelia/issues/656), refs [#644](https://github.com/netresearch/ofelia/pull/644), [#645](https://github.com/netresearch/ofelia/pull/645), [#586](https://github.com/netresearch/ofelia/pull/586), [#635](https://github.com/netresearch/ofelia/issues/635))

### Refactor

- Unify Docker host / scheme resolution in `core/adapters/docker/client.go` into a single `resolveDockerHost` seam. `NewClientWithConfig` and `createHTTPClient` now agree on the resolved host without re-reading `DOCKER_HOST`, eliminating the dual-reader anti-pattern that produced [#605](https://github.com/netresearch/ofelia/issues/605) / [#607](https://github.com/netresearch/ofelia/issues/607) / [#609](https://github.com/netresearch/ofelia/issues/609). The dispatch `switch` and the separate `supportedDockerHostSchemes` slice collapsed into a single `schemeHandlers` map (allow-list + dispatch derived from the same data); scheme spelling lives in named constants; `formatSupportedSchemes` is cached in a package var. `client.FromEnv` is dropped from the SDK options chain (host + TLS are mirrored explicitly; `DOCKER_API_VERSION` is preserved via `client.WithVersionFromEnv()`). New contract test asserts `DOCKER_HOST` is read at most once per `NewClientWithConfig` call; new parity test asserts the public allow-list cannot drift from the dispatch table. Pure refactor with one minor operator-visible side effect: the `unsupported DOCKER_HOST scheme` error now lists the supported schemes in alphabetical order (`http://, https://, npipe://, tcp://, unix://`) rather than the previous curated `unix, tcp, http, https, npipe` order — the new map-derived list sorts deterministically. ([#617](https://github.com/netresearch/ofelia/issues/617))

## [0.24.0] - 2026-05-10

### Changed

- **BREAKING:** Docker Compose service-name based job naming now works as documented. The `com.docker.compose.service` label is no longer filtered out, so the `Cross-Container Job References (Docker Compose)` feature from `docs/CONFIGURATION.md` is functional. Users who relied on the previous (incorrect) job names may see different names. ([#597](https://github.com/netresearch/ofelia/pull/597))

### Added

- End-to-end test harness running the compiled binary as a subprocess — covers scheduling, the `validate` command, SIGTERM/SIGINT graceful shutdown, and real Alpine container runs ([#581](https://github.com/netresearch/ofelia/pull/581))

### Fixed

- `log-level` invalid-value error now lists all accepted levels ([#599](https://github.com/netresearch/ofelia/pull/599))
- `make lint` works again — `golangci-lint` is now installed via the v2 module path ([#600](https://github.com/netresearch/ofelia/pull/600))
- `.envrc` hooks detection inside git worktrees ([#598](https://github.com/netresearch/ofelia/pull/598))
- `.gitignore` `/ofelia` pattern is anchored so it cannot shadow source files ([#574](https://github.com/netresearch/ofelia/pull/574))
- Stabilize flaky tests for scheduler shutdown, retry backoff, and rate limiter ([#582](https://github.com/netresearch/ofelia/pull/582), [#601](https://github.com/netresearch/ofelia/pull/601))

### Security

- Bump Go to 1.26.2 for stdlib security fixes ([#557](https://github.com/netresearch/ofelia/pull/557))

### Dependencies

- Bump `github.com/netresearch/go-cron` 0.13.1 → 0.14.0 ([#553](https://github.com/netresearch/ofelia/pull/553), [#563](https://github.com/netresearch/ofelia/pull/563))
- Bump `github.com/docker/cli` 29.3.0 → 29.4.0 ([#548](https://github.com/netresearch/ofelia/pull/548), [#559](https://github.com/netresearch/ofelia/pull/559))
- Bump `github.com/docker/go-connections` 0.6.0 → 0.7.0 ([#564](https://github.com/netresearch/ofelia/pull/564))
- Bump `github.com/go-playground/validator/v10` 10.30.1 → 10.30.2 ([#552](https://github.com/netresearch/ofelia/pull/552))
- Bump `github.com/go-viper/mapstructure/v2` 2.4.0 → 2.5.0 ([#549](https://github.com/netresearch/ofelia/pull/549))
- Bump `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` 1.42.0 → 1.43.0 ([#556](https://github.com/netresearch/ofelia/pull/556))
- Bump `golang.org/x/crypto`, `golang.org/x/term`, `golang.org/x/text` ([#558](https://github.com/netresearch/ofelia/pull/558), [#560](https://github.com/netresearch/ofelia/pull/560), [#561](https://github.com/netresearch/ofelia/pull/561))
- Bump go-dependencies group ([#596](https://github.com/netresearch/ofelia/pull/596))
- Bump `alpine` Docker base image ([#569](https://github.com/netresearch/ofelia/pull/569))
- Bump GitHub Actions groups ([#550](https://github.com/netresearch/ofelia/pull/550), [#554](https://github.com/netresearch/ofelia/pull/554), [#562](https://github.com/netresearch/ofelia/pull/562))

### CI / Build

- Adopt unified single-build release pipeline via `netresearch/.github` reusable workflows ([#566](https://github.com/netresearch/ofelia/pull/566), [#587](https://github.com/netresearch/ofelia/pull/587))
- Migrate auto-merge to org-level reusable workflow ([#567](https://github.com/netresearch/ofelia/pull/567))
- Drop `integration.yml` — superseded by `go-check` ([#579](https://github.com/netresearch/ofelia/pull/579))
- Stop Trivy FS scan from blocking PRs on pre-existing CVEs ([#555](https://github.com/netresearch/ofelia/pull/555))
- Fix auto-merge for Dependabot/Renovate PRs ([#551](https://github.com/netresearch/ofelia/pull/551))
- Use cosign `--bundle` for checksums signing ([#547](https://github.com/netresearch/ofelia/pull/547))
- Grant `security-events: write` to satisfy reusable workflow ([#585](https://github.com/netresearch/ofelia/pull/585))

### Refactor

- Extract repeated string literals flagged by `goconst` ([#599](https://github.com/netresearch/ofelia/pull/599))

## [0.23.1] - 2026-03-23

### Fixed

- Migrate release pipeline from `slsa-github-generator` to `actions/attest-build-provenance` via org-wide reusable workflow — fixes release builds blocked by SHA-pinning ruleset ([#542](https://github.com/netresearch/ofelia/pull/542))

### Security

- Migrate `go-viper/mapstructure` v1 to v2.4.0 — fixes GO-2025-3787 and GO-2025-3900 (sensitive information leak in logs) ([#544](https://github.com/netresearch/ofelia/pull/544))

## [0.23.0] - 2026-03-22

### Added

- `env-file` support: load environment variables from files for all job types, like Docker's `--env-file` ([#540](https://github.com/netresearch/ofelia/pull/540), closes [#314](https://github.com/netresearch/ofelia/issues/314))
- `env-from` support: copy environment variables from a running Docker container at job execution time ([#540](https://github.com/netresearch/ofelia/pull/540), closes [#336](https://github.com/netresearch/ofelia/issues/336), [#351](https://github.com/netresearch/ofelia/issues/351))

### Fixed

- Environment variable substitutions containing `#` or `;` were parsed as INI inline comments, truncating values like SMTP passwords ([#539](https://github.com/netresearch/ofelia/pull/539), fixes [#538](https://github.com/netresearch/ofelia/issues/538))
- Environment variable expansion now works in webhook config values (`secret`, `url`, etc.) and section names ([#539](https://github.com/netresearch/ofelia/pull/539))
- `log-level` config value now supports `${VAR}` expansion in the pre-parse path ([#539](https://github.com/netresearch/ofelia/pull/539))

### Security

- SHA-pin all GitHub Actions and add Dependabot for actions updates ([#536](https://github.com/netresearch/ofelia/pull/536))

### Dependencies

- Bump the github-actions group with 20 updates ([#537](https://github.com/netresearch/ofelia/pull/537))

## [0.22.0] - 2026-03-20

### Added

- Environment variable substitution in INI config files with `${VAR}` and `${VAR:-default}` syntax ([#532](https://github.com/netresearch/ofelia/pull/532), closes [#362](https://github.com/netresearch/ofelia/issues/362))

### Dependencies

- Bump `aquasecurity/trivy-action` from 0.28.0 to v0.35.0 ([#532](https://github.com/netresearch/ofelia/pull/532))
- Bump `step-security/harden-runner` from v2.12.0 to v2.16.0 ([#533](https://github.com/netresearch/ofelia/pull/533))
- Bump `codecov/codecov-action` from v5.5.2 to v5.5.3 ([#533](https://github.com/netresearch/ofelia/pull/533))
- Bump `go.opentelemetry.io/otel` from v1.40.0 to v1.42.0 ([#533](https://github.com/netresearch/ofelia/pull/533))
- Bump `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` from v1.38.0 to v1.42.0 ([#533](https://github.com/netresearch/ofelia/pull/533))
- Bump `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` from v0.65.0 to v0.67.0 ([#533](https://github.com/netresearch/ofelia/pull/533))
- Bump `go.opentelemetry.io/proto/otlp` from v1.9.0 to v1.10.0 ([#533](https://github.com/netresearch/ofelia/pull/533))
- Bump `google.golang.org/protobuf` from v1.36.10 to v1.36.11 ([#533](https://github.com/netresearch/ofelia/pull/533))
- Bump `google.golang.org/grpc` from v1.77.0 to v1.79.3 ([#531](https://github.com/netresearch/ofelia/pull/531))

## [0.21.5] - 2026-03-18

### Added

- `ofelia version` command and `--version` flag ([#528](https://github.com/netresearch/ofelia/pull/528))
- `job-service-run` now supports `volume` for mounting host directories and named volumes ([#529](https://github.com/netresearch/ofelia/pull/529), closes [#527](https://github.com/netresearch/ofelia/issues/527))

## [0.21.4] - 2026-03-17

### Fixed

- Fix `job-service-run` network not attached to service ([#525](https://github.com/netresearch/ofelia/pull/525), closes [#524](https://github.com/netresearch/ofelia/issues/524))
  - `convertToSwarmSpec` now reads networks from both `ServiceSpec.Networks` and `TaskTemplate.Networks`
- Complete `convertFromSwarmService` with missing field conversions: Mounts, RestartPolicy, Resources, Networks, Mode, Placement, LogDriver, EndpointSpec ([#525](https://github.com/netresearch/ofelia/pull/525))

### Added

- Swarm service adapter now converts Placement, LogDriver, and EndpointSpec in both directions ([#525](https://github.com/netresearch/ofelia/pull/525))
- 13 round-trip tests for the service adapter conversion layer ([#525](https://github.com/netresearch/ofelia/pull/525))

## [0.21.3] - 2026-03-15

### Fixed

- Wire missing container spec fields across job types ([#520](https://github.com/netresearch/ofelia/pull/520), closes [#519](https://github.com/netresearch/ofelia/issues/519))
  - `job-service-run`: add `environment`, `hostname`, `dir` support
  - `job-run`: add `working-dir` support, wire `volumes-from` (was in struct but unused)
  - `job-exec`: add `privileged` support
  - Fix misleading documentation claiming `job-service-run` inherits from `RunJob`

## [0.21.2] - 2026-03-14

### Security

- Hide `WebPasswordHash` and `WebSecretKey` from `/api/config` endpoint ([#511](https://github.com/netresearch/ofelia/pull/511))
- Remove CSRF bypass via `X-Requested-With` header ([#511](https://github.com/netresearch/ofelia/pull/511))
- Implement rate limiter cleanup to prevent memory exhaustion DoS ([#511](https://github.com/netresearch/ofelia/pull/511))
- Only trust `X-Forwarded-For` and `X-Real-IP` from trusted proxies to prevent IP spoofing ([#511](https://github.com/netresearch/ofelia/pull/511))
- Make trusted proxies configurable via `web-trusted-proxies` ([#511](https://github.com/netresearch/ofelia/pull/511))

### Fixed

- Propagate context to Docker API calls so cancellation and shutdown reach containers ([#511](https://github.com/netresearch/ofelia/pull/511))
- Prevent double-close panic on daemon done channel ([#511](https://github.com/netresearch/ofelia/pull/511))
- Add mutex to Config to prevent concurrent map access crash ([#511](https://github.com/netresearch/ofelia/pull/511))
- Execute shutdown hooks in priority groups, not all concurrently ([#511](https://github.com/netresearch/ofelia/pull/511))
- Enforce shutdown timeout even when hooks ignore context ([#511](https://github.com/netresearch/ofelia/pull/511))
- Return `NonZeroExitError` for non-zero Swarm service exit codes ([#511](https://github.com/netresearch/ofelia/pull/511))

### Dependencies

- Bump `golang.org/x/crypto` from 0.48.0 to 0.49.0 ([#512](https://github.com/netresearch/ofelia/pull/512))
- Bump `github.com/netresearch/go-cron` from 0.13.0 to 0.13.1 ([#514](https://github.com/netresearch/ofelia/pull/514))
- Bump `golang.org/x/time` from 0.14.0 to 0.15.0 ([#515](https://github.com/netresearch/ofelia/pull/515))

## [0.17.0] - 2025-12-22

### Added

- **Secure Web Authentication** ([#408](https://github.com/netresearch/ofelia/pull/408))
  - Complete bcrypt password hashing with HMAC session tokens
  - Secure cookie handling with HttpOnly, Secure, and SameSite flags
  - Support for reverse proxy HTTPS detection (X-Forwarded-Proto)
  - Password hashing utility: `ofelia hashpw`

- **Doctor Command Enhancements** ([#408](https://github.com/netresearch/ofelia/pull/408))
  - Web authentication configuration checks in `ofelia doctor`
  - Validates password hash format and token secret strength

- **ntfy-token Preset** ([#409](https://github.com/netresearch/ofelia/pull/409))
  - Bearer token authentication for self-hosted ntfy instances
  - Supports both ntfy.sh and self-hosted deployments with access tokens

- **Webhook Host Whitelist** ([#410](https://github.com/netresearch/ofelia/pull/410))
  - New `webhook-allowed-hosts` configuration option
  - Default: `*` (allow all hosts) - consistent with local command trust model
  - Whitelist mode when specific hosts are configured
  - Supports domain wildcards (e.g., `*.slack.com`)

- **CronClock Interface** ([#412](https://github.com/netresearch/ofelia/pull/412))
  - Testable time abstraction for scheduler testing
  - FakeClock implementation for instant, deterministic tests
  - go-cron compatible Timer interface

### Security

- **Cookie Security Hardening** ([#411](https://github.com/netresearch/ofelia/pull/411))
  - Secure, HttpOnly, and SameSite=Lax flags on all cookies
  - HTTPS detection for reverse proxy deployments
  - Security boundaries ADR documenting responsibility model

- **GitHub Actions Pinning** ([#411](https://github.com/netresearch/ofelia/pull/411))
  - All workflow actions pinned to SHA for supply chain security
  - CodeQL updated to v3.31.9

### Improved

- **Test Infrastructure** ([#412](https://github.com/netresearch/ofelia/pull/412))
  - Complete gocheck to stdlib+testify migration
  - Eventually pattern replacing time.Sleep-based synchronization
  - Parallel test execution with t.Parallel()
  - Race condition fixes detected by -race flag

- **Performance** ([#412](https://github.com/netresearch/ofelia/pull/412))
  - Sub-second scheduling for faster test execution
  - Optimized pre-commit and pre-push hooks
  - Test suite runtime reduced by ~80%

- **Linting** ([#413](https://github.com/netresearch/ofelia/pull/413))
  - Comprehensive golangci-lint configuration audit
  - All linting issues resolved

### Documentation

- **Security Boundaries ADR** ([#411](https://github.com/netresearch/ofelia/pull/411))
  - ADR-002 documenting security responsibility model
  - Clear separation between Ofelia and infrastructure responsibilities

- **Webhook Documentation** ([#410](https://github.com/netresearch/ofelia/pull/410))
  - Host whitelist configuration guide
  - Security model explanation

## [0.16.0] - 2025-12-10

### Fixed

- **Docker Socket HTTP/2 Compatibility**
  - Fixed Docker client connection failures on non-TLS connections introduced in v0.11.0
  - OptimizedDockerClient now only enables HTTP/2 for HTTPS (TLS) connections
  - HTTP/2 is disabled for Unix sockets, tcp://, and http:// (Docker daemon only supports HTTP/2 over TLS with ALPN)
  - Resolves "protocol error" issues when connecting to `/var/run/docker.sock` or `tcp://localhost:2375`
  - HTTP/2 enabled only for `https://` connections where Docker daemon supports ALPN negotiation
  - Added comprehensive unit tests covering all connection types (9 scenarios)
  - Technical details: Docker daemon does not implement h2c (HTTP/2 cleartext) - HTTP/2 requires TLS

## [0.11.0] - 2025-11-21

### Critical Fixes

- **Command Parsing in Swarm Services** ([#254](https://github.com/netresearch/ofelia/pull/254))
  - Fixed critical bug where `strings.Split` broke quoted arguments in Docker Swarm service commands
  - Now uses `args.GetArgs()` to properly handle commands like `sh -c "echo hello world"`
  - Prevents command execution failures in complex shell commands

- **LocalJob Empty Command Panic** ([#254](https://github.com/netresearch/ofelia/pull/254))
  - Fixed documented bug where empty commands caused runtime panic
  - Now returns proper error instead of crashing
  - Prevents service crashes from malformed job configurations

### Security

- **API Security Validation** ([#254](https://github.com/netresearch/ofelia/pull/254))
  - Added validation for LocalJob and ComposeJob API endpoints
  - Prevents command injection attacks via API
  - Validates file paths, service names, and command arguments

- **Privilege Escalation Logging** ([#244](https://github.com/netresearch/ofelia/pull/244))
  - Enhanced logging for security monitoring
  - Better detection of privilege escalation attempts

- **Dependency Updates**
  - Updated golang.org/x/crypto to v0.45.0 for CVE fixes

### Performance

- **Enhanced Buffer Pool** ([#245](https://github.com/netresearch/ofelia/pull/245))
  - Multi-tier adaptive pooling system
  - 99.97% memory usage reduction (2000 MB → 0.5 MB for 100 executions)
  - Automatic size adjustment and pool warmup

- **Optimized Docker Client** ([#245](https://github.com/netresearch/ofelia/pull/245))
  - Connection pooling for reduced overhead
  - Thread-safe concurrent operations
  - Health monitoring and automatic recovery

- **Reduced Polling** ([#254](https://github.com/netresearch/ofelia/pull/254))
  - Increased legacy polling interval from 500ms to 2s
  - 75% reduction in Docker API calls (200/min → 50/min per job)
  - Significant CPU and network usage improvement

- **Performance Metrics Framework** ([#245](https://github.com/netresearch/ofelia/pull/245))
  - Comprehensive metrics for Docker operations
  - Memory, latency, and throughput tracking
  - Real-time performance monitoring

### Added

- **Container Annotations**
  - Support for custom annotations on RunJob and RunServiceJob
  - Default Ofelia annotations for job tracking
  - User-defined metadata for containers and services

- **WorkingDir for ExecJob**
  - Support for setting working directory in exec jobs
  - Backward compatible with existing configurations

- **Opt-in Validation**
  - New `enable-strict-validation` flag
  - Allows gradual migration to strict validation
  - Prevents breaking changes for existing users

- **Git Hooks with Lefthook**
  - Go-native git hooks for better portability
  - Pre-commit, commit-msg, pre-push, post-checkout, post-merge hooks
  - Automated code quality checks and security scans

### Documentation

- **Architecture Diagrams** ([#252](https://github.com/netresearch/ofelia/pull/252))
  - System architecture overview
  - Component interaction diagrams
  - Data flow visualization

- **Complete Package Documentation** ([#247](https://github.com/netresearch/ofelia/pull/247))
  - Comprehensive package-level documentation
  - Security guides and best practices
  - Practical usage guides

- **Docker Requirements**
  - Documented minimum Docker version requirements
  - API compatibility notes

- **Exit Code Documentation** ([#254](https://github.com/netresearch/ofelia/pull/254))
  - Clear documentation of Ofelia-specific exit codes
  - Swarm service error codes (-999, -998)

### Fixed

- **Go Version Check** ([#251](https://github.com/netresearch/ofelia/pull/251))
  - Corrected inverted logic in .envrc Go version check
  - Ensures correct Go version enforcement

### Changed

- Updated go-dockerclient to v1.12.2
- Migrated from Husky to Lefthook for git hooks
- Improved CI/CD pipeline with comprehensive security scanning

### Internal

- Removed AI assistant artifacts and outdated documentation ([#246](https://github.com/netresearch/ofelia/pull/246), [#253](https://github.com/netresearch/ofelia/pull/253))
- Enhanced test suite with comprehensive integration tests
- Improved code organization and maintainability

## [0.10.2] - 2025-11-15

Previous release.

---

## Migration Guide v0.10.x → v0.11.0

### Breaking Changes

**None** - This release is backward compatible with v0.10.x

### Recommended Actions

1. **Review API Usage**: If you create jobs via API, ensure commands are properly validated
2. **Check Swarm Commands**: Verify complex shell commands in service jobs work correctly
3. **Monitor Performance**: Observe improved memory usage and reduced API calls
4. **Enable Metrics**: Consider enabling the new metrics framework for monitoring

### New Configuration Options

```ini
# Optional: Enable strict validation (default: false)
[global]
enable-strict-validation = true

# New: Container annotations
[job-run "example"]
annotations = com.example.key=value, app.version=1.0
```

### Deprecations

**None** in this release.

---

For more information, see:
- [Documentation](https://github.com/netresearch/ofelia/tree/main/docs)
- [Security Guide](https://github.com/netresearch/ofelia/blob/main/docs/SECURITY.md)
- [Configuration Guide](https://github.com/netresearch/ofelia/blob/main/docs/CONFIGURATION.md)
