# Webhook Notifications

Ofelia supports sending webhook notifications when jobs complete. You can configure multiple named webhooks and assign them to specific jobs, allowing flexible notification routing.

## Lifecycle at a glance

Three asymmetries are easy to miss when reading the sections below in
isolation: (1) webhook *definitions* are operator-controlled and only
processed from a service container or from INI, while *references* can
come from any container; (2) INI definitions override label definitions on
name collision; (3) at dispatch time the per-webhook `url` field
short-circuits the preset's `url_scheme`. The diagram below traces the
full flow from "where do definitions come from" to "what hits the wire".

```mermaid
flowchart TD
    subgraph Sources["📝 Define webhooks (operator-controlled)"]
        direction LR
        INI["<b>INI</b><br/><code>[webhook &quot;NAME&quot;]</code><br/>preset = …<br/>url = …<br/>id / secret / trigger / …"]
        Labels["<b>Docker labels</b><br/><code>ofelia.webhook.NAME.preset</code><br/><code>ofelia.webhook.NAME.url</code><br/>… on a container with<br/><code>ofelia.service: &quot;true&quot;</code> only"]
    end

    INI ==>|"INI wins on<br/>name collision<br/>(warn-logged)"| Registry
    Labels ==> Registry

    Registry[("<b>Webhook registry</b><br/>keyed by name<br/>(WebhookConfigs.Webhooks)")]

    subgraph Refs["🔗 Reference webhooks by name (any container)"]
        direction LR
        JobRef["<b>Per job</b><br/><code>ofelia.job-exec.JOB.webhooks: a, b</code><br/>or <code>webhooks = a, b</code> in INI"]
        GlobalRef["<b>Global selector</b><br/><code>[global] webhook-webhooks = c</code><br/>or <code>ofelia.webhook-webhooks</code> label"]
    end

    JobRef --> Union
    GlobalRef --> Union
    Union{{"Per-job union<br/>(deduped by name)"}}
    Registry -.->|"name lookup<br/>(unknown → attach error)"| Union

    Union --> Fallback{"<code>preset</code> on<br/>this webhook empty?"}
    Fallback -->|no| Attach
    Fallback -->|yes| DefaultPreset{"<code>webhook-default-preset</code><br/>set?"}
    DefaultPreset -->|"<b>nil</b> = unset<br/>(default)"| FillJSONPost["fill with bundled<br/><code>json-post</code>"]
    DefaultPreset -->|"<b>non-empty</b><br/>operator's choice"| FillCustom["fill with operator's<br/>chosen preset name"]
    DefaultPreset -->|"<b>empty string</b><br/>(explicit opt-out)"| AttachError["<b>Attach error</b><br/>names <code>webhook-default-preset</code><br/>for grep-ability"]
    FillJSONPost --> Attach
    FillCustom --> Attach

    Attach["<b>Composite middleware</b><br/>attached once per job"]

    Attach --> Dispatch["<b>On job run, per inner webhook:</b><br/>1. <code>ShouldNotify(trigger)</code><br/>2. <code>BuildURL</code> — <code>url</code> overrides <code>url_scheme</code><br/>3. <code>ValidateWebhookURL</code> (allow-list)<br/>4. HTTP send + retry"]

    classDef src fill:#fef3c7,stroke:#d97706
    classDef ref fill:#dbeafe,stroke:#2563eb
    classDef reg fill:#dcfce7,stroke:#16a34a
    classDef err fill:#fee2e2,stroke:#dc2626
    class Sources src
    class Refs ref
    class Registry reg
    class AttachError err
```

Where to read next:

- **Source authoring** — INI `[webhook "name"]` or service-container
  labels: see [Quick Start](#quick-start) and
  [Docker Label Reference](#docker-label-reference).
- **Security boundary** — why labels are service-container-only and what
  the operator-tunable globals are: see
  [INI vs Docker labels](#docker-label-reference) and
  [`webhook-allowed-hosts`](#global-settings).
- **The `json-post` fallback** —
  [Custom Webhooks (URL-only, no custom preset)](#custom-webhooks-url-only-no-custom-preset)
  and [Opting out of the default](#opting-out-of-the-default).
- **`url` overriding `url_scheme`** — the column annotation in
  [Webhook Settings](#webhook-settings).

## Quick Start

### Simplest case: just POST to a URL

If you only need to POST a JSON payload to an existing webhook endpoint
(Healthchecks.io, Better Stack, a custom internal service, etc.), set
`url =` and skip `preset` entirely — Ofelia uses the bundled `json-post`
preset and posts a structured JSON body. No custom preset YAML needed.

```ini
[webhook "healthchecks"]
url = https://hc.example.com/ping/00000000-0000-0000-0000-000000000000
trigger = always

[job-exec "backup-database"]
schedule = @daily
container = postgres
command = pg_dump -U postgres mydb > /backup/db.sql
webhooks = healthchecks
```

See [Custom Webhooks](#custom-webhooks-url-only-no-custom-preset) for the
exact payload shape and how to opt out of the fallback.

### With a bundled preset (Slack, Discord, Teams, Matrix, etc.)

Use `preset = slack` (or `discord` / `teams` / `matrix` / `ntfy` /
`gotify` / `pushover` / `pagerduty`) when the receiver expects a
service-specific payload format:

```ini
[global]
; Global webhook settings (optional)
webhook-allow-remote-presets = false

[webhook "slack-alerts"]
preset = slack
id = T00000000/B00000000000
secret = XXXXXXXXXXXXXXXXXXXXXXXX
trigger = error

[job-exec "backup-database"]
schedule = @daily
container = postgres
command = pg_dump -U postgres mydb > /backup/db.sql
webhooks = slack-alerts
```

### Docker Labels

```yaml
services:
  ofelia:
    image: ghcr.io/netresearch/ofelia:latest
    labels:
      ofelia.enabled: "true"
      ofelia.service: "true"

      # Global webhook settings (use the same webhook-* names as the INI [global] section)
      ofelia.webhook-webhooks: "slack-alerts"
      ofelia.webhook-allowed-hosts: "hooks.slack.com,discord.com"

      # Define webhooks
      ofelia.webhook.slack-alerts.preset: slack
      ofelia.webhook.slack-alerts.id: "T00000000/B00000000000"
      ofelia.webhook.slack-alerts.secret: "XXXXXXXXXXXXXXXXXXXXXXXX"
      ofelia.webhook.slack-alerts.trigger: error

      ofelia.webhook.discord-notify.preset: discord
      ofelia.webhook.discord-notify.id: "1234567890123456789"
      ofelia.webhook.discord-notify.secret: "abcdefghijklmnopqrstuvwxyz"
      ofelia.webhook.discord-notify.trigger: always

  worker:
    image: myapp:latest
    labels:
      ofelia.enabled: "true"
      # Assign webhooks to a job
      ofelia.job-exec.backup.schedule: "@daily"
      ofelia.job-exec.backup.container: postgres
      ofelia.job-exec.backup.command: "pg_dump -U postgres mydb > /backup/db.sql"
      ofelia.job-exec.backup.webhooks: "slack-alerts, discord-notify"
```

> **Important**: Webhook labels (`ofelia.webhook.*`) are only processed from the **service container** (the container with `ofelia.service: "true"`). Webhook labels on non-service containers are ignored.

### Docker Label Reference

All webhook parameters can be set via Docker labels on the service container:

| Label | Description |
|-------|-------------|
| `ofelia.webhook.NAME.preset` | Preset name (slack, discord, etc.) |
| `ofelia.webhook.NAME.id` | Service-specific identifier |
| `ofelia.webhook.NAME.secret` | Service-specific secret/token |
| `ofelia.webhook.NAME.url` | Custom webhook URL |
| `ofelia.webhook.NAME.trigger` | When to send: `always`, `error`, `success`, `skipped` |
| `ofelia.webhook.NAME.timeout` | HTTP request timeout (e.g., `30s`) |
| `ofelia.webhook.NAME.retry-count` | Number of retries on failure |
| `ofelia.webhook.NAME.retry-delay` | Delay between retries (e.g., `5s`) |
| `ofelia.webhook.NAME.link` | Optional URL to include in notification |
| `ofelia.webhook.NAME.link-text` | Display text for link |

Two operator-tunable, non-SSRF-sensitive webhook globals are exposed via
Docker labels. The SSRF-sensitive globals (`webhook-allowed-hosts`,
`webhook-allow-remote-presets`, `webhook-trusted-preset-sources`,
`webhook-preset-cache-dir`) must be set via the INI `[global]` section. The
label names use the same `webhook-*` prefix as the INI keys:

| Label | Description |
|-------|-------------|
| `ofelia.webhook-webhooks` | Default webhooks for all jobs (comma-separated) |
| `ofelia.webhook-preset-cache-ttl` | Cache lifetime for remote presets (e.g. `12h`). INI value wins on conflict. |
| `ofelia.webhook-default-preset` | Preset name used when a webhook omits `preset` (default: `json-post`, the bundled JSON POST preset). Set to empty to opt out — every webhook must then declare `preset` explicitly. INI value wins on conflict. See [Custom Webhooks](#custom-webhooks-url-only-no-custom-preset). |

> **Security:** the SSRF-sensitive webhook globals listed above are intentionally **not** accepted from container labels to prevent a malicious container from widening the network egress surface or pointing the preset cache at an attacker-controlled directory. They must be set in the INI `[global]` section. See [#486](https://github.com/netresearch/ofelia/issues/486).

> **Deprecated:** the unprefixed legacy form `ofelia.webhooks` is still accepted for backward compatibility but logs a one-shot deprecation warning. Migrate to the `webhook-` prefixed form shown above. The other unprefixed legacy forms (`ofelia.allow-remote-presets`, `ofelia.trusted-preset-sources`, `ofelia.preset-cache-ttl`, `ofelia.preset-cache-dir`) were never accepted from labels because their canonical forms are INI-only. See [#620](https://github.com/netresearch/ofelia/issues/620).

### Configuration Precedence

When both INI and Docker labels define a webhook with the same name, the **INI configuration takes precedence**. Label-defined webhooks with conflicting names are ignored with a warning. This prevents container labels from hijacking credentials defined in the INI file.

### Dynamic Updates

When Docker container events are enabled (`--docker-events`), webhook configurations from labels are automatically synced when containers start, stop, or change. If webhook labels are added or modified, the webhook manager is re-initialized and all job middlewares are rebuilt.

## Bundled Presets

Ofelia includes presets for popular notification services:

| Preset | Service | Required Variables |
|--------|---------|-------------------|
| `slack` | Slack Incoming Webhooks | `id`, `secret` |
| `discord` | Discord Webhooks | `id`, `secret` |
| `teams` | Microsoft Teams | `url` |
| `matrix` | Matrix (via hookshot bridge) | `url` |
| `ntfy` | ntfy.sh (public topics) | `id` (topic) |
| `ntfy-token` | ntfy.sh (with Bearer auth) | `id` (topic), `secret` (access token) |
| `pushover` | Pushover | `id` (user key), `secret` (API token) |
| `pagerduty` | PagerDuty Events API v2 | `secret` (routing key) |
| `gotify` | Gotify | `url`, `secret` (app token) |

## Configuration Reference

### Webhook Settings

| Option | Type | Description |
|--------|------|-------------|
| `preset` | string | Preset name (bundled or remote) |
| `url` | string | Custom webhook URL (overrides preset URL) |
| `id` | string | Service-specific identifier |
| `secret` | string | Service-specific secret/token |
| `link` | string | Optional URL to include in notification (e.g., link to logs) |
| `link-text` | string | Display text for link (default: "View Details") |
| `trigger` | string | When to send: `always`, `error`, `success`, `skipped` (default: `error`) |
| `timeout` | duration | HTTP request timeout (default: `30s`) |
| `retry-count` | int | Number of retries on failure (default: `3`) |
| `retry-delay` | duration | Delay between retries (default: `5s`) |

> **Shutdown behavior:** Retries and in-flight HTTP requests both observe scheduler cancellation, so `SIGTERM` drains the webhook stack promptly rather than blocking for `retry-delay × retry-count` (plus one `timeout` for the in-flight request). Since v0.25.1 ([#673](https://github.com/netresearch/ofelia/issues/673)).

### Job Webhook Assignment

Assign webhooks to jobs using the `webhooks` option:

```ini
[job-exec "my-job"]
schedule = @hourly
container = myapp
command = /run-task.sh
webhooks = slack-alerts, discord-notify
```

Multiple webhooks can be assigned (comma-separated).

### Global Settings

```ini
[global]
; Comma-separated list of webhook names applied to every job by default
; (jobs can still add to or override this with their own `webhooks = ...` key).
; This is the only webhook-* global key that can also be set via Docker labels
; (as `ofelia.webhook-webhooks`); the others are INI-only for SSRF safety.
webhook-webhooks =

; Allow fetching presets from remote URLs (default: false)
webhook-allow-remote-presets = false

; Comma-separated SSRF allow-list of remote preset sources, evaluated against
; the preset string before any fetch when `webhook-allow-remote-presets = true`.
; Supports glob patterns. Empty (default) blocks all remote fetches even when
; remote presets are enabled — you must opt in explicitly.
;
; SECURITY: treat this as an SSRF allow-list — Ofelia will issue outbound HTTP(S)
; requests on behalf of whoever controls the preset string. Prefer the most
; specific patterns possible; avoid bare `*` or `https://*`, and never set this
; to a wildcard that would match cloud-metadata endpoints (`http://169.254.169.254/...`)
; or internal services.
;
; Examples: `gh:netresearch/*`, `gh:myorg/ofelia-presets/*`,
; `https://presets.example.com/*`.
; INI-only — never accepted from Docker labels (see #486 / #620).
webhook-trusted-preset-sources =

; Cache TTL for remote presets (default: 24h)
webhook-preset-cache-ttl = 24h

; Directory used to cache fetched remote presets. Default:
; `$XDG_CACHE_HOME/ofelia/presets` when `XDG_CACHE_HOME` is set, otherwise the
; system temp directory (e.g. `/tmp`).
;
; SECURITY: use a directory writable ONLY by the Ofelia process (e.g.
; `/var/cache/ofelia/presets`, owned 0700 by the daemon user). Anyone who
; can write here can plant preset files that Ofelia will load on the next
; cache hit and execute as middleware config. When bind-mounting host paths,
; the directory inherits host ACLs — verify nothing else can write there.
;
; INI-only — never accepted from Docker labels (a malicious container could
; otherwise repoint the cache at an attacker-controlled directory). See #486 /
; #620.
webhook-preset-cache-dir =

; Comma-separated whitelist of webhook target hosts. Default: `*` (allow all
; hosts — consistent with the local command execution trust model). Supports
; wildcards (`*.example.com`). When set to a specific list, requests to any
; other host are blocked at delivery time. INI-only.
webhook-allowed-hosts = *

; Preset name used when a webhook configuration omits `preset` (default:
; `json-post`, the bundled JSON POST preset that turns a bare `url = ...`
; entry into a working webhook without authoring a custom preset).
; Set to an empty string to opt out — in that case every webhook MUST set
; `preset` explicitly or attachment fails with a logged error.
; Operator-tunable; also accepted via Docker label `ofelia.webhook-default-preset`.
webhook-default-preset = json-post
```

> **INI vs Docker labels:** the operator-tunable, non-SSRF-sensitive globals are
> all also accepted via Docker labels on the Ofelia service container:
>
> - `ofelia.webhook-webhooks` — global selector
> - `ofelia.webhook-preset-cache-ttl` — remote preset cache lifetime
> - `ofelia.webhook-default-preset` — fallback when a webhook omits `preset` (see #676)
>
> The SSRF-sensitive keys (`webhook-trusted-preset-sources`, `webhook-preset-cache-dir`,
> `webhook-allowed-hosts`, `webhook-allow-remote-presets`) remain INI-only and
> must be set in the INI `[global]` section so a malicious container cannot widen
> the network egress surface or repoint preset loading. See
> [#486](https://github.com/netresearch/ofelia/issues/486),
> [#620](https://github.com/netresearch/ofelia/issues/620), and
> [#640](https://github.com/netresearch/ofelia/issues/640).

## Preset Examples

### Slack

```ini
[webhook "slack-alerts"]
preset = slack
id = T00000000/B00000000000
secret = XXXXXXXXXXXXXXXXXXXXXXXX
trigger = error
```

The `id` is your workspace/channel identifier and `secret` is the webhook token from your Slack Incoming Webhook URL:
`https://hooks.slack.com/services/{id}/{secret}`

### Discord

```ini
[webhook "discord-notify"]
preset = discord
id = 1234567890123456789
secret = abcdefghijklmnopqrstuvwxyz1234567890ABCDEF
trigger = always
```

From your Discord webhook URL: `https://discord.com/api/webhooks/{id}/{secret}`

### Microsoft Teams

```ini
[webhook "teams-alerts"]
preset = teams
url = https://outlook.office.com/webhook/your-webhook-url
trigger = error
```

### Matrix (via hookshot)

```ini
[webhook "matrix-alerts"]
preset = matrix
url = https://matrix.example.com/hookshot/webhooks/webhook/your-webhook-id
trigger = error
link = https://logs.example.com/ofelia
link-text = View Logs
```

The Matrix preset works with the [matrix-hookshot](https://github.com/matrix-org/matrix-hookshot) bridge. Create a webhook in your Matrix room and use the full webhook URL.

The optional `link` and `link-text` fields add a clickable link to your notifications, useful for linking to log dashboards or job details.

### ntfy

For public topics on ntfy.sh (no authentication):
```ini
[webhook "ntfy-notify"]
preset = ntfy
id = my-topic-name
trigger = always
```

For private topics or self-hosted ntfy with access tokens:
```ini
[webhook "ntfy-private"]
preset = ntfy-token
id = my-private-topic
secret = tk_AgQdq7mVBoFD37zQVN29RhuMzNIz2
trigger = always
```

For self-hosted ntfy with custom URL and authentication:
```ini
[webhook "ntfy-self-hosted"]
preset = ntfy-token
url = https://ntfy.example.com/my-topic
secret = tk_AgQdq7mVBoFD37zQVN29RhuMzNIz2
trigger = always
```

> **Note**: Use `ntfy` for public topics without authentication, and `ntfy-token` when
> Bearer token authentication is required (self-hosted instances with access control
> or private topics on ntfy.sh).

### Pushover

```ini
[webhook "pushover-alerts"]
preset = pushover
id = user-key-here
secret = api-token-here
trigger = error
```

### PagerDuty

```ini
[webhook "pagerduty-oncall"]
preset = pagerduty
secret = routing-key-here
trigger = error
```

### Gotify

```ini
[webhook "gotify-notify"]
preset = gotify
url = https://gotify.example.com
secret = app-token-here
trigger = always
```

## Custom Webhooks (URL-only, no custom preset)

You can configure a webhook with just a `url =` and Ofelia will POST a JSON
payload describing the job execution to that URL — no preset authoring
required. This works because every webhook that omits `preset` falls back
to the bundled `json-post` preset (see `[global] webhook-default-preset`).

```ini
[webhook "custom-hook"]
url = https://api.example.com/webhook
trigger = always
timeout = 10s
retry-count = 2
```

Equivalent Docker labels (on the Ofelia service container):

```yaml
labels:
  ofelia.service: "true"
  ofelia.enabled: "true"
  ofelia.webhook.custom-hook.url: "https://api.example.com/webhook"
  ofelia.webhook.custom-hook.trigger: "always"
```

### Payload shape

The bundled `json-post` preset POSTs `Content-Type: application/json; charset=utf-8`
with a body shaped like:

```json
{
  "job": {
    "name": "backup",
    "command": "/run-backup.sh",
    "schedule": "@hourly",
    "type": "exec"
  },
  "execution": {
    "id": "abc123",
    "status": "successful",
    "failed": false,
    "skipped": false,
    "duration": "1.234s",
    "duration_ns": 1234000000,
    "start_time": "2026-05-15T14:00:00+02:00",
    "end_time": "2026-05-15T14:00:01+02:00",
    "error": "",
    "output": "...",
    "stderr": ""
  },
  "host": { "hostname": "...", "timestamp": "..." },
  "ofelia": { "version": "..." }
}
```

`output` and `stderr` are truncated to 4 000 characters each. If `link =`
is set on the webhook config, a `link` object is included.

### Opting out of the default

Use cases for opting out:

- **Audit policy** — your security team requires every outbound webhook to
  declare its preset explicitly, so silent fallbacks during config drift
  cannot surprise a future audit.
- **"Ban implicit fallback"** — you want a noisy startup error when an
  operator forgets `preset` instead of unintended JSON traffic to whatever
  is in `url`.
- **Bring-your-own-default** — you maintain a local preset (e.g. a
  Loki-formatted JSON shape) and want every webhook that omits `preset` to
  resolve to *that*, not `json-post`. Set `webhook-default-preset = my-loki`
  instead of `=` (empty).

To opt out entirely (require explicit `preset` on every webhook), set the
global selector to empty:

```ini
[global]
webhook-default-preset =
```

After opting out, any webhook that omits `preset` fails attachment with a
logged error that names `webhook-default-preset` so operators can grep
straight to this section. A `url = ...` alone is not enough once the
fallback is disabled: the preset is required, and `url` only overrides
the preset's `url_scheme`.

## Remote Presets

> **Security Warning**: Remote presets execute templates that could potentially exfiltrate data. Only enable this feature if you trust the preset sources.

Enable remote presets in global settings:

```ini
[global]
webhook-allow-remote-presets = true
webhook-preset-cache-ttl = 24h
```

### GitHub Shorthand

Reference presets from GitHub using shorthand notation:

```ini
[webhook "custom-service"]
; Loads from github.com/user/repo/blob/main/presets/custom.yaml
preset = gh:user/repo/presets/custom.yaml
```

### Full URL

```ini
[webhook "custom-service"]
preset = https://raw.githubusercontent.com/user/repo/main/presets/custom.yaml
```

## Template Variables

Webhook body templates have access to the following data:

### Job Data (`.Job`)

| Variable | Type | Description |
|----------|------|-------------|
| `.Job.Name` | string | Job name |
| `.Job.Command` | string | Executed command |
| `.Job.Schedule` | string | Cron schedule |
| `.Job.Container` | string | Container name (if applicable) |

### Execution Data (`.Execution`)

| Variable | Type | Description |
|----------|------|-------------|
| `.Execution.Status` | string | `successful`, `failed`, or `skipped` |
| `.Execution.Failed` | bool | Whether execution failed |
| `.Execution.Skipped` | bool | Whether execution was skipped |
| `.Execution.Error` | string | Error message (if failed) |
| `.Execution.Duration` | duration | Execution duration |
| `.Execution.StartTime` | time.Time | When execution started |
| `.Execution.EndTime` | time.Time | When execution ended |

### Host Data (`.Host`)

| Variable | Type | Description |
|----------|------|-------------|
| `.Host.Hostname` | string | Machine hostname |
| `.Host.Timestamp` | time.Time | Current timestamp |

### Ofelia Data (`.Ofelia`)

| Variable | Type | Description |
|----------|------|-------------|
| `.Ofelia.Version` | string | Ofelia version |

### Preset Data (`.Preset`)

| Variable | Type | Description |
|----------|------|-------------|
| `.Preset.ID` | string | Configured ID value |
| `.Preset.Secret` | string | Configured secret value |
| `.Preset.URL` | string | Configured URL value |
| `.Preset.Link` | string | Configured link URL (empty if not set) |
| `.Preset.LinkText` | string | Configured link text (defaults to "View Details") |

## Template Functions

Templates support these helper functions:

| Function | Description | Example |
|----------|-------------|---------|
| `json` | JSON-escape a string | `{{json .Execution.Error}}` |
| `truncate` | Limit string length | `{{truncate 100 .Execution.Error}}` |
| `isoTime` | Format time as ISO 8601 | `{{isoTime .Host.Timestamp}}` |
| `unixTime` | Format time as Unix timestamp | `{{unixTime .Host.Timestamp}}` |
| `formatDuration` | Format duration as string | `{{formatDuration .Execution.Duration}}` |

## Creating Custom Presets

Custom presets use YAML format:

```yaml
name: my-service
description: "My custom notification service"
version: "1.0.0"

url_scheme: "https://api.myservice.com/notify/{id}"

method: POST
headers:
  Content-Type: "application/json"
  Authorization: "Bearer {secret}"

variables:
  id:
    description: "Service ID"
    required: true
  secret:
    description: "API token"
    required: true
    sensitive: true

body: |
  {
    "title": "Job {{.Job.Name}} {{.Execution.Status}}",
    "message": "{{if .Execution.Failed}}Error: {{.Execution.Error}}{{else}}Completed in {{.Execution.Duration}}{{end}}",
    "timestamp": "{{isoTime .Host.Timestamp}}"
  }
```

### Preset Schema

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Preset identifier |
| `description` | string | Human-readable description |
| `version` | string | Preset version |
| `url_scheme` | string | URL template with `{id}`, `{secret}` placeholders |
| `method` | string | HTTP method (GET, POST, etc.) |
| `headers` | map | HTTP headers (supports `{secret}` placeholder) |
| `variables` | map | Variable definitions with validation |
| `body` | string | Go template for request body |

## Security

### Security Model

Ofelia's webhook security follows the same trust model as local command execution: **if you control the configuration, you control the behavior**. Since Ofelia already trusts users to run arbitrary commands on the host or in containers, it applies the same trust level to webhook destinations.

### Host Whitelist

The `webhook-allowed-hosts` setting controls which hosts webhooks can target:

| Value | Behavior |
|-------|----------|
| `*` (default) | Allow all hosts - webhooks can target any URL |
| Specific hosts | Whitelist mode - only listed hosts are allowed |

#### Default: Allow All Hosts

```ini
[global]
; Default behavior - all hosts allowed (no config needed)
webhook-allowed-hosts = *
```

Webhooks can target any host including `192.168.x.x`, `10.x.x.x`, `localhost`, etc.

#### Whitelist Mode

For multi-tenant or cloud deployments, restrict webhooks to specific hosts:

```ini
[global]
webhook-allowed-hosts = hooks.slack.com, discord.com, ntfy.sh, 192.168.1.20
```

Only the listed hosts can receive webhooks. Supports domain wildcards:

```ini
[global]
webhook-allowed-hosts = *.slack.com, *.internal.example.com
```

#### Configuration Reference

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `webhook-allowed-hosts` | string | `*` | Host whitelist. `*` = allow all, specific list = whitelist mode. Supports domain wildcards (`*.example.com`) |

### Best Practices

1. **Keep secrets secure**: Use environment variables or secret management for webhook credentials
2. **Use HTTPS**: Always use HTTPS URLs for production webhooks
3. **Limit remote presets**: Keep `webhook-allow-remote-presets = false` unless necessary
4. **Audit presets**: Review remote preset sources before enabling them
5. **Use whitelist in cloud**: Set `webhook-allowed-hosts` to specific hosts for multi-tenant deployments

## Migration from Slack Middleware

If you're using the deprecated `slack-webhook` option, migrate to the new webhook system:

### Before (Deprecated)

```ini
[job-exec "my-job"]
schedule = @hourly
container = myapp
command = /run-task.sh
slack-webhook = https://hooks.slack.com/services/TXXXX/BXXXX/your-secret-here
slack-only-on-error = true
```

### After (New System)

```ini
[webhook "slack"]
preset = slack
id = T00000000/B00000000000
secret = XXXXXXXXXXXXXXXXXXXXXXXX
trigger = error

[job-exec "my-job"]
schedule = @hourly
container = myapp
command = /run-task.sh
webhooks = slack
```

The deprecated `slack-webhook` option will continue to work but will show a deprecation warning. It will be removed in a future version.

## Troubleshooting

### Webhook not sending

1. Check the `trigger` setting matches your expected condition
2. Verify the webhook is assigned to the job with `webhooks = webhook-name`
3. Check Ofelia logs for webhook errors

### Authentication errors

1. Verify `id` and `secret` values are correct
2. Check if the service requires additional authentication headers
3. Try using a custom `url` to bypass preset URL construction

### Timeout errors

Increase the timeout for slow services:

```ini
[webhook "slow-service"]
preset = slack
timeout = 60s
retry-count = 5
retry-delay = 10s
```

### Host not allowed (whitelist mode)

If you've configured `webhook-allowed-hosts` with specific hosts and get "host not in allowed hosts list":

1. **Add the host to the whitelist**:
   ```ini
   [global]
   webhook-allowed-hosts = hooks.slack.com, 192.168.1.20, ntfy.local
   ```

2. **Allow all hosts** (default behavior):
   ```ini
   [global]
   webhook-allowed-hosts = *
   ```

See the [Host Whitelist](#host-whitelist) section for details.
