# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `job-exec` jobs can now set the initial pseudo-TTY console size via two new fields: `console-height` (rows) and `console-width` (columns). Useful for jobs that render TUIs, tables, or formatted text — applications that expect a specific terminal geometry (`htop`, `vim`, formatted-output reports) now render correctly. Both default to 0, meaning "use Docker's default size" (preserving pre-fix behavior). Only honored when `tty = true`; otherwise the Docker daemon silently ignores the size. Requires Docker API v1.42+ (Docker Engine 20.10+, released 2020-12) — Ofelia's auto-negotiation handles older daemons gracefully. Closes [#235](https://github.com/netresearch/ofelia/issues/235).

- `job-run` jobs can now customize the signal Ofelia sends to the container's main process at stop time via a new `stop-signal` field. Accepts the canonical name (`SIGINT`, `SIGUSR1`) or the bare suffix (`INT`, `USR1`); empty (the default) preserves the pre-fix behavior of falling back to whatever the container image declared via `STOPSIGNAL` (which itself defaults to `SIGTERM`). Useful for apps with signal handlers — Node.js workers that handle `SIGINT` cleanly, Java apps that dump threads on `SIGQUIT`, or custom cleanup paths on `SIGUSR1`/`SIGUSR2`. Pair with the existing `stop-timeout` to give the application enough time to flush state before `SIGKILL`. Requires Docker API v1.42+ (Docker Engine 20.10+, released 2020-12). Closes [#234](https://github.com/netresearch/ofelia/issues/234).

- New `[docker] startup-retry-count` and `[docker] startup-retry-interval` config (also exposed as `--docker-startup-retry-count` / `OFELIA_DOCKER_STARTUP_RETRY_COUNT` and `--docker-startup-retry-interval` / `OFELIA_DOCKER_STARTUP_RETRY_INTERVAL`) retry the initial Docker connection with exponential backoff before the daemon exits. Default is `startup-retry-count=0`, which preserves the pre-fix "exit on first failure" behavior; setting `startup-retry-count=5 startup-retry-interval=1s` yields a 1s → 2s → 4s → 8s → 16s budget (~31s total). Each per-attempt ping is still bounded by `dockerStartupPingTimeout` (10s) so a wedged daemon cannot inflate startup beyond `(count+1)·10s + Σbackoffs`. The backoff window observes ctx cancellation so SIGTERM during startup drains promptly instead of blocking the full budget (same shape as [#685](https://github.com/netresearch/ofelia/pull/685) / [#687](https://github.com/netresearch/ofelia/pull/687)). Useful for TCP-based Docker hosts where the daemon may briefly be unreachable on startup before health checks settle (socket proxies, remote Docker hosts, Docker-in-Docker). Closes [#523](https://github.com/netresearch/ofelia/issues/523).

### Changed

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

### Removed

- **BREAKING (source-only, pre-1.0):** Removed unused `core/adapters/docker.ClientConfig.HTTPClient` field that was declared in [#681](https://github.com/netresearch/ofelia/pull/681) but never read — a caller setting `cfg.HTTPClient = someClient` silently got the auto-constructed transport instead of theirs. Downstream Go consumers that referenced the field in named struct literals or assignments will see a compile-time error after upgrade (semantically a no-op since the field was already ignored at runtime); permitted under SemVer for the current 0.y.z line (cf. [SemVer §4](https://semver.org/#spec-item-4)). Removing the field turns the silent footgun into a loud compile-time error rather than preserving it as a deprecated no-op. If you need a transport-level injection seam, file a feature request with the use case so the suppression of `disableHTTP2AutoConfig` on caller-supplied transports (the [#668](https://github.com/netresearch/ofelia/issues/668) invariant) can be wired in correctly. ([#693](https://github.com/netresearch/ofelia/pull/693), closes [#684](https://github.com/netresearch/ofelia/issues/684))

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
