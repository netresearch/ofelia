package ports

import (
	"context"

	"github.com/netresearch/ofelia/core/domain"
)

// SystemService provides operations for Docker system information.
type SystemService interface {
	// Info returns system-wide information.
	Info(ctx context.Context) (*domain.SystemInfo, error)

	// Ping pings the Docker server.
	Ping(ctx context.Context) (*domain.PingResponse, error)

	// Version returns version information.
	Version(ctx context.Context) (*domain.Version, error)

	// DiskUsage returns disk usage information.
	DiskUsage(ctx context.Context) (*domain.DiskUsage, error)
}
