package cli

import "errors"

// Validation errors
var (
	ErrWebhookNameRequired = errors.New("webhook section must have a name")
	ErrWebAuthUsername     = errors.New("web-auth-enabled requires web-username to be set")
	ErrWebAuthPassword     = errors.New("web-auth-enabled requires web-password-hash to be set")
	ErrInvalidDockerFilter = errors.New("invalid docker filter format")
	ErrHealthCheckFailed   = errors.New("health check failed")
	ErrInvalidBcryptCost   = errors.New("bcrypt cost out of valid range")
	ErrPasswordTooShort    = errors.New("password must be at least 8 characters")
	ErrPasswordMismatch    = errors.New("passwords do not match")
	ErrUsernameEmpty       = errors.New("username cannot be empty")
	ErrUsernameTooShort    = errors.New("username must be at least 3 characters")
	ErrJobNameEmpty        = errors.New("job name cannot be empty")
	ErrJobNameInvalid      = errors.New("job name must be alphanumeric with hyphens or underscores only")
	ErrDockerImageEmpty    = errors.New("Docker image cannot be empty")
	ErrCommandEmpty        = errors.New("command cannot be empty")
	ErrScheduleEmpty       = errors.New("schedule cannot be empty")
)
