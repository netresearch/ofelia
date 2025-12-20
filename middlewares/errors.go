package middlewares

import "errors"

// Preset errors
var (
	ErrPresetEmpty        = errors.New("preset specification cannot be empty")
	ErrPresetNotFound     = errors.New("preset not found")
	ErrRemoteDisabled     = errors.New("remote presets are disabled")
	ErrUntrustedSource    = errors.New("preset source not in trusted sources")
	ErrPresetFetchFailed  = errors.New("failed to fetch preset")
	ErrPresetTooLarge     = errors.New("preset file too large")
	ErrPresetInvalid      = errors.New("preset must have either url_scheme or body defined")
	ErrUnreplacedVars     = errors.New("URL contains unreplaced variables")
	ErrCacheExpired       = errors.New("cache expired")
	ErrCacheCollision     = errors.New("cache key collision")
	ErrNotGitHubShorthand = errors.New("not a GitHub shorthand")
	ErrInvalidGitHub      = errors.New("invalid GitHub shorthand format")
)

// Webhook errors
var (
	ErrWebhookNameEmpty   = errors.New("webhook name cannot be empty")
	ErrWebhookNotFound    = errors.New("webhook not found")
	ErrMissingVariable    = errors.New("required variable not provided")
	ErrWebhookHTTPFailed  = errors.New("webhook HTTP request failed")
	ErrMissingPresetOrURL = errors.New("either preset or url must be specified")
	ErrInvalidTrigger     = errors.New("invalid trigger type")
	ErrNegativeTimeout    = errors.New("timeout cannot be negative")
	ErrNegativeRetryCount = errors.New("retry-count cannot be negative")
	ErrNegativeRetryDelay = errors.New("retry-delay cannot be negative")
)

// Webhook security errors
var (
	ErrInvalidURLScheme = errors.New("URL scheme must be http or https")
	ErrMissingHost      = errors.New("URL must have a host")
	ErrMissingHostname  = errors.New("URL must have a hostname")
	ErrHostNotAllowed   = errors.New("host is not in allowed hosts list")
)

// Sanitize errors
var (
	ErrDangerousPattern = errors.New("invalid path: contains dangerous pattern")
	ErrSystemDirectory  = errors.New("invalid path: cannot write to system directory")
)
