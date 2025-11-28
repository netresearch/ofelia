package mock

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/netresearch/ofelia/core/domain"
)

// ImageService is a mock implementation of ports.ImageService.
type ImageService struct {
	mu sync.RWMutex

	// Callbacks for customizing behavior
	OnPull        func(ctx context.Context, opts domain.PullOptions) (io.ReadCloser, error)
	OnPullAndWait func(ctx context.Context, opts domain.PullOptions) error
	OnList        func(ctx context.Context, opts domain.ImageListOptions) ([]domain.ImageSummary, error)
	OnInspect     func(ctx context.Context, imageID string) (*domain.Image, error)
	OnRemove      func(ctx context.Context, imageID string, force, pruneChildren bool) error
	OnExists      func(ctx context.Context, imageRef string) (bool, error)

	// Call tracking
	PullCalls        []domain.PullOptions
	PullAndWaitCalls []domain.PullOptions
	ListCalls        []domain.ImageListOptions
	InspectCalls     []string
	RemoveCalls      []ImageRemoveCall
	ExistsCalls      []string

	// Simulated data
	Images       []domain.ImageSummary
	ExistsResult bool
}

// ImageRemoveCall represents a call to Remove().
type ImageRemoveCall struct {
	ImageID       string
	Force         bool
	PruneChildren bool
}

// NewImageService creates a new mock ImageService.
func NewImageService() *ImageService {
	return &ImageService{
		ExistsResult: true, // Default: images exist
	}
}

// Pull pulls an image.
func (s *ImageService) Pull(ctx context.Context, opts domain.PullOptions) (io.ReadCloser, error) {
	s.mu.Lock()
	s.PullCalls = append(s.PullCalls, opts)
	s.mu.Unlock()

	if s.OnPull != nil {
		return s.OnPull(ctx, opts)
	}

	// Return a simple progress response
	progress := `{"status":"Pulling from library/alpine"}
{"status":"Digest: sha256:mock"}
{"status":"Status: Downloaded newer image for alpine:latest"}
`
	return io.NopCloser(bytes.NewBufferString(progress)), nil
}

// PullAndWait pulls an image and waits for completion.
func (s *ImageService) PullAndWait(ctx context.Context, opts domain.PullOptions) error {
	s.mu.Lock()
	s.PullAndWaitCalls = append(s.PullAndWaitCalls, opts)
	s.mu.Unlock()

	if s.OnPullAndWait != nil {
		return s.OnPullAndWait(ctx, opts)
	}

	// Simulate reading the pull stream
	reader, err := s.Pull(ctx, opts)
	if err != nil {
		return err
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)
	return nil
}

// List lists images.
func (s *ImageService) List(ctx context.Context, opts domain.ImageListOptions) ([]domain.ImageSummary, error) {
	s.mu.Lock()
	s.ListCalls = append(s.ListCalls, opts)
	images := s.Images
	s.mu.Unlock()

	if s.OnList != nil {
		return s.OnList(ctx, opts)
	}
	return images, nil
}

// Inspect returns image information.
func (s *ImageService) Inspect(ctx context.Context, imageID string) (*domain.Image, error) {
	s.mu.Lock()
	s.InspectCalls = append(s.InspectCalls, imageID)
	s.mu.Unlock()

	if s.OnInspect != nil {
		return s.OnInspect(ctx, imageID)
	}
	return &domain.Image{
		ID:       imageID,
		RepoTags: []string{imageID},
	}, nil
}

// Remove removes an image.
func (s *ImageService) Remove(ctx context.Context, imageID string, force, pruneChildren bool) error {
	s.mu.Lock()
	s.RemoveCalls = append(s.RemoveCalls, ImageRemoveCall{
		ImageID:       imageID,
		Force:         force,
		PruneChildren: pruneChildren,
	})
	s.mu.Unlock()

	if s.OnRemove != nil {
		return s.OnRemove(ctx, imageID, force, pruneChildren)
	}
	return nil
}

// Tag tags an image.
func (s *ImageService) Tag(ctx context.Context, source, target string) error {
	return nil
}

// Exists checks if an image exists.
func (s *ImageService) Exists(ctx context.Context, imageRef string) (bool, error) {
	s.mu.Lock()
	s.ExistsCalls = append(s.ExistsCalls, imageRef)
	result := s.ExistsResult
	s.mu.Unlock()

	if s.OnExists != nil {
		return s.OnExists(ctx, imageRef)
	}
	return result, nil
}

// SetImages sets the images returned by List().
func (s *ImageService) SetImages(images []domain.ImageSummary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Images = images
}

// SetExistsResult sets the result returned by Exists().
func (s *ImageService) SetExistsResult(exists bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ExistsResult = exists
}
