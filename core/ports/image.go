package ports

import (
	"context"
	"io"

	"github.com/netresearch/ofelia/core/domain"
)

// ImageService provides operations for managing Docker images.
type ImageService interface {
	// Pull pulls an image from a registry.
	// The returned ReadCloser contains pull progress and must be closed by the caller.
	// The progress can be decoded as JSON-encoded PullProgress messages.
	Pull(ctx context.Context, opts domain.PullOptions) (io.ReadCloser, error)

	// PullAndWait pulls an image and waits for completion.
	// This is a convenience method that handles the progress stream.
	PullAndWait(ctx context.Context, opts domain.PullOptions) error

	// List returns a list of images matching the options.
	List(ctx context.Context, opts domain.ImageListOptions) ([]domain.ImageSummary, error)

	// Inspect returns detailed information about an image.
	Inspect(ctx context.Context, imageID string) (*domain.Image, error)

	// Remove removes an image.
	Remove(ctx context.Context, imageID string, force, pruneChildren bool) error

	// Tag tags an image.
	Tag(ctx context.Context, source, target string) error

	// Exists checks if an image exists locally.
	Exists(ctx context.Context, imageRef string) (bool, error)
}

// AuthProvider provides authentication for registry operations.
type AuthProvider interface {
	// GetAuthConfig returns the authentication configuration for a registry.
	GetAuthConfig(registry string) (domain.AuthConfig, error)

	// GetEncodedAuth returns base64-encoded authentication for a registry.
	GetEncodedAuth(registry string) (string, error)
}
