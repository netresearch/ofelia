// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/netresearch/ofelia/core/domain"
)

// ImageServiceAdapter implements ports.ImageService using Docker SDK.
type ImageServiceAdapter struct {
	client *client.Client
}

// checkClient returns ErrNilDockerClient if the embedded SDK client is nil.
// See docker.ErrNilDockerClient for rationale.
func (s *ImageServiceAdapter) checkClient() error {
	if s.client == nil {
		return ErrNilDockerClient
	}
	return nil
}

// Pull pulls an image from a registry.
func (s *ImageServiceAdapter) Pull(ctx context.Context, opts domain.PullOptions) (io.ReadCloser, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	pullOpts := client.ImagePullOptions{
		RegistryAuth: opts.RegistryAuth,
	}
	// PullOptions.Platform (a string) became Platforms ([]ocispec.Platform).
	// No ofelia caller sets it today; an unparseable value is dropped, leaving
	// the daemon to pick the platform as it did for an empty string.
	if plat, ok := parsePlatform(opts.Platform); ok {
		pullOpts.Platforms = []ocispec.Platform{plat}
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
	if err := s.checkClient(); err != nil {
		return err
	}
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

// parsePlatform parses an "os/arch[/variant]" platform string. It reports
// false for the empty string and for anything without at least os and arch.
func parsePlatform(s string) (ocispec.Platform, bool) {
	parts := strings.Split(s, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ocispec.Platform{}, false
	}
	plat := ocispec.Platform{OS: parts[0], Architecture: parts[1]}
	if len(parts) > 2 {
		plat.Variant = parts[2]
	}
	return plat, true
}

// List lists images.
func (s *ImageServiceAdapter) List(ctx context.Context, opts domain.ImageListOptions) ([]domain.ImageSummary, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	listOpts := client.ImageListOptions{
		All: opts.All,
	}

	listOpts.Filters = toSDKFilters(opts.Filters)

	images, err := s.client.ImageList(ctx, listOpts)
	if err != nil {
		return nil, convertError(err)
	}

	result := make([]domain.ImageSummary, len(images.Items))
	for i, img := range images.Items {
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
	if err := s.checkClient(); err != nil {
		return nil, err
	}
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
	if err := s.checkClient(); err != nil {
		return err
	}
	_, err := s.client.ImageRemove(ctx, imageID, client.ImageRemoveOptions{
		Force:         force,
		PruneChildren: pruneChildren,
	})
	return convertError(err)
}

// Tag tags an image.
func (s *ImageServiceAdapter) Tag(ctx context.Context, source, target string) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	_, err := s.client.ImageTag(ctx, client.ImageTagOptions{Source: source, Target: target})
	return convertError(err)
}

// Exists checks if an image exists locally.
func (s *ImageServiceAdapter) Exists(ctx context.Context, imageRef string) (bool, error) {
	if err := s.checkClient(); err != nil {
		return false, err
	}
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
		Username: auth.Username,
		Password: auth.Password,
		Auth:     auth.Auth,
		// registry.AuthConfig dropped the long-deprecated Email field. Nothing
		// is lost: convertAuthConfig, the only producer of a domain.AuthConfig
		// from the credential store, never populated it either. The domain type
		// keeps the field so the ports API is unchanged.
		ServerAddress: auth.ServerAddress,
		IdentityToken: auth.IdentityToken,
		RegistryToken: auth.RegistryToken,
	}

	// The Docker API expects the credentials as base64-encoded JSON in the
	// X-Registry-Auth header, so the password field has to be marshaled here.
	// #nosec G117 -- registry auth payload required by the Docker API
	encoded, err := json.Marshal(authConfig)
	if err != nil {
		return "", fmt.Errorf("encoding auth config: %w", err)
	}

	return base64.URLEncoding.EncodeToString(encoded), nil
}
