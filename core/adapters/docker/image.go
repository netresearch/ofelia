package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"

	"github.com/netresearch/ofelia/core/domain"
)

// ImageServiceAdapter implements ports.ImageService using Docker SDK.
type ImageServiceAdapter struct {
	client *client.Client
}

// Pull pulls an image from a registry.
func (s *ImageServiceAdapter) Pull(ctx context.Context, opts domain.PullOptions) (io.ReadCloser, error) {
	pullOpts := image.PullOptions{
		RegistryAuth: opts.RegistryAuth,
		Platform:     opts.Platform,
	}

	ref := opts.Repository
	if opts.Tag != "" {
		ref = ref + ":" + opts.Tag
	}

	reader, err := s.client.ImagePull(ctx, ref, pullOpts)
	if err != nil {
		return nil, convertError(err)
	}

	return reader, nil
}

// PullAndWait pulls an image and waits for completion.
func (s *ImageServiceAdapter) PullAndWait(ctx context.Context, opts domain.PullOptions) error {
	reader, err := s.Pull(ctx, opts)
	if err != nil {
		return err
	}
	defer reader.Close()

	// Consume the stream to wait for completion
	if _, err = io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("reading image pull response: %w", err)
	}
	return nil
}

// List lists images.
func (s *ImageServiceAdapter) List(ctx context.Context, opts domain.ImageListOptions) ([]domain.ImageSummary, error) {
	listOpts := image.ListOptions{
		All: opts.All,
	}

	if len(opts.Filters) > 0 {
		listOpts.Filters = filters.NewArgs()
		for key, values := range opts.Filters {
			for _, v := range values {
				listOpts.Filters.Add(key, v)
			}
		}
	}

	images, err := s.client.ImageList(ctx, listOpts)
	if err != nil {
		return nil, convertError(err)
	}

	result := make([]domain.ImageSummary, len(images))
	for i, img := range images {
		result[i] = domain.ImageSummary{
			ID:          img.ID,
			ParentID:    img.ParentID,
			RepoTags:    img.RepoTags,
			RepoDigests: img.RepoDigests,
			Created:     img.Created,
			Size:        img.Size,
			SharedSize:  img.SharedSize,
			Labels:      img.Labels,
			Containers:  img.Containers,
		}
	}

	return result, nil
}

// Inspect returns image information.
func (s *ImageServiceAdapter) Inspect(ctx context.Context, imageID string) (*domain.Image, error) {
	img, err := s.client.ImageInspect(ctx, imageID)
	if err != nil {
		return nil, convertError(err)
	}

	return &domain.Image{
		ID:          img.ID,
		RepoTags:    img.RepoTags,
		RepoDigests: img.RepoDigests,
		Comment:     img.Comment,
		Created:     parseTime(img.Created),
		Size:        img.Size,
		Labels:      img.Config.Labels,
	}, nil
}

// Remove removes an image.
func (s *ImageServiceAdapter) Remove(ctx context.Context, imageID string, force, pruneChildren bool) error {
	_, err := s.client.ImageRemove(ctx, imageID, image.RemoveOptions{
		Force:         force,
		PruneChildren: pruneChildren,
	})
	return convertError(err)
}

// Tag tags an image.
func (s *ImageServiceAdapter) Tag(ctx context.Context, source, target string) error {
	err := s.client.ImageTag(ctx, source, target)
	return convertError(err)
}

// Exists checks if an image exists locally.
func (s *ImageServiceAdapter) Exists(ctx context.Context, imageRef string) (bool, error) {
	_, err := s.client.ImageInspect(ctx, imageRef)
	if err != nil {
		if domain.IsNotFound(convertError(err)) {
			return false, nil
		}
		return false, convertError(err)
	}
	return true, nil
}

// EncodeAuthConfig encodes an auth config for use in API calls.
func EncodeAuthConfig(auth domain.AuthConfig) (string, error) {
	authConfig := registry.AuthConfig{
		Username:      auth.Username,
		Password:      auth.Password,
		Auth:          auth.Auth,
		Email:         auth.Email,
		ServerAddress: auth.ServerAddress,
		IdentityToken: auth.IdentityToken,
		RegistryToken: auth.RegistryToken,
	}

	encoded, err := json.Marshal(authConfig)
	if err != nil {
		return "", fmt.Errorf("encoding auth config: %w", err)
	}

	return base64.URLEncoding.EncodeToString(encoded), nil
}
