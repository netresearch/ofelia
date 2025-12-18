# Webhook Notifications

Ofelia supports sending webhook notifications when jobs complete. You can configure multiple named webhooks and assign them to specific jobs, allowing flexible notification routing.

## Quick Start

### INI Configuration

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
    image: mcuadros/ofelia:latest
    labels:
      # Define a webhook
      ofelia.webhook.slack-alerts.preset: slack
      ofelia.webhook.slack-alerts.id: "T00000000/B00000000000"
      ofelia.webhook.slack-alerts.secret: "XXXXXXXXXXXXXXXXXXXXXXXX"
      ofelia.webhook.slack-alerts.trigger: error

      # Assign webhook to a job
      ofelia.job-exec.backup.schedule: "@daily"
      ofelia.job-exec.backup.container: postgres
      ofelia.job-exec.backup.command: "pg_dump -U postgres mydb > /backup/db.sql"
      ofelia.job-exec.backup.webhooks: slack-alerts
```

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
; Allow fetching presets from remote URLs
webhook-allow-remote-presets = false

; Cache TTL for remote presets
webhook-preset-cache-ttl = 24h
```

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

## Custom Webhooks

You can configure webhooks without a preset by providing a URL directly:

```ini
[webhook "custom-hook"]
url = https://api.example.com/webhook
trigger = always
timeout = 10s
retry-count = 2
```

This sends a JSON payload with job execution data to the specified URL.

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
