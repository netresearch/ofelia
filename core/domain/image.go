package domain

import (
	"io"
	"time"
)

// Image represents a Docker image.
type Image struct {
	ID          string
	RepoTags    []string
	RepoDigests []string
	Parent      string
	Comment     string
	Created     time.Time
	Container   string
	Size        int64
	VirtualSize int64
	Labels      map[string]string
}

// ImageSummary represents a summary of an image for list operations.
type ImageSummary struct {
	ID          string
	ParentID    string
	RepoTags    []string
	RepoDigests []string
	Created     int64
	Size        int64
	SharedSize  int64
	VirtualSize int64
	Labels      map[string]string
	Containers  int64
}

// PullOptions represents options for pulling an image.
type PullOptions struct {
	// Repository to pull (e.g., "alpine", "nginx:latest")
	Repository string

	// Tag to pull (if not included in repository)
	Tag string

	// Platform to pull (e.g., "linux/amd64")
	Platform string

	// RegistryAuth is base64 encoded auth config
	RegistryAuth string
}

// ImageListOptions represents options for listing images.
type ImageListOptions struct {
	All     bool              // Show all images (default hides intermediate)
	Filters map[string][]string // Filters to apply
}

// PullProgress represents progress information during an image pull.
type PullProgress struct {
	Status         string
	ProgressDetail ProgressDetail
	ID             string
	Error          string
}

// ProgressDetail represents detailed progress information.
type ProgressDetail struct {
	Current int64
	Total   int64
}

// PullReader provides methods to read image pull progress.
type PullReader interface {
	io.ReadCloser
}

// AuthConfig contains authorization information for connecting to a registry.
type AuthConfig struct {
	Username      string
	Password      string
	Auth          string // Base64 encoded "username:password"
	Email         string
	ServerAddress string
	IdentityToken string
	RegistryToken string
}

// AuthConfigurations contains a map of registry addresses to auth configs.
type AuthConfigurations struct {
	Configs map[string]AuthConfig
}

// ParsedReference represents a parsed image reference.
type ParsedReference struct {
	Repository string
	Tag        string
	Digest     string
}

// ParseRepositoryTag parses a repository:tag string into its components.
func ParseRepositoryTag(repoTag string) ParsedReference {
	ref := ParsedReference{
		Tag: "latest",
	}

	// Find the last @ for digest
	if idx := lastIndex(repoTag, '@'); idx >= 0 {
		ref.Repository = repoTag[:idx]
		ref.Digest = repoTag[idx+1:]
		return ref
	}

	// Find the last : for tag, but be careful of port numbers
	// in the registry (e.g., localhost:5000/image:tag)
	lastColon := lastIndex(repoTag, ':')
	lastSlash := lastIndex(repoTag, '/')

	if lastColon >= 0 && lastColon > lastSlash {
		ref.Repository = repoTag[:lastColon]
		ref.Tag = repoTag[lastColon+1:]
	} else {
		ref.Repository = repoTag
	}

	return ref
}

func lastIndex(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
