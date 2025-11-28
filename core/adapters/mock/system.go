package mock

import (
	"context"
	"sync"

	"github.com/netresearch/ofelia/core/domain"
)

// SystemService is a mock implementation of ports.SystemService.
type SystemService struct {
	mu sync.RWMutex

	// Callbacks for customizing behavior
	OnInfo      func(ctx context.Context) (*domain.SystemInfo, error)
	OnPing      func(ctx context.Context) (*domain.PingResponse, error)
	OnVersion   func(ctx context.Context) (*domain.Version, error)
	OnDiskUsage func(ctx context.Context) (*domain.DiskUsage, error)

	// Call tracking
	InfoCalls      int
	PingCalls      int
	VersionCalls   int
	DiskUsageCalls int

	// Simulated data
	InfoResult      *domain.SystemInfo
	PingResult      *domain.PingResponse
	VersionResult   *domain.Version
	DiskUsageResult *domain.DiskUsage

	// Errors
	InfoErr      error
	PingErr      error
	VersionErr   error
	DiskUsageErr error
}

// NewSystemService creates a new mock SystemService.
func NewSystemService() *SystemService {
	return &SystemService{
		InfoResult: &domain.SystemInfo{
			ID:            "mock-docker-id",
			Name:          "mock-docker",
			ServerVersion: "24.0.0",
			NCPU:          4,
			MemTotal:      16000000000,
		},
		PingResult: &domain.PingResponse{
			APIVersion: "1.44",
			OSType:     "linux",
		},
		VersionResult: &domain.Version{
			Version:    "24.0.0",
			APIVersion: "1.44",
			Os:         "linux",
			Arch:       "amd64",
		},
	}
}

// Info returns system information.
func (s *SystemService) Info(ctx context.Context) (*domain.SystemInfo, error) {
	s.mu.Lock()
	s.InfoCalls++
	info := s.InfoResult
	err := s.InfoErr
	s.mu.Unlock()

	if s.OnInfo != nil {
		return s.OnInfo(ctx)
	}
	if err != nil {
		return nil, err
	}
	return info, nil
}

// Ping pings the Docker server.
func (s *SystemService) Ping(ctx context.Context) (*domain.PingResponse, error) {
	s.mu.Lock()
	s.PingCalls++
	ping := s.PingResult
	err := s.PingErr
	s.mu.Unlock()

	if s.OnPing != nil {
		return s.OnPing(ctx)
	}
	if err != nil {
		return nil, err
	}
	return ping, nil
}

// Version returns version information.
func (s *SystemService) Version(ctx context.Context) (*domain.Version, error) {
	s.mu.Lock()
	s.VersionCalls++
	version := s.VersionResult
	err := s.VersionErr
	s.mu.Unlock()

	if s.OnVersion != nil {
		return s.OnVersion(ctx)
	}
	if err != nil {
		return nil, err
	}
	return version, nil
}

// DiskUsage returns disk usage information.
func (s *SystemService) DiskUsage(ctx context.Context) (*domain.DiskUsage, error) {
	s.mu.Lock()
	s.DiskUsageCalls++
	usage := s.DiskUsageResult
	err := s.DiskUsageErr
	s.mu.Unlock()

	if s.OnDiskUsage != nil {
		return s.OnDiskUsage(ctx)
	}
	if err != nil {
		return nil, err
	}
	return usage, nil
}

// SetInfoResult sets the result returned by Info().
func (s *SystemService) SetInfoResult(info *domain.SystemInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InfoResult = info
}

// SetInfoError sets the error returned by Info().
func (s *SystemService) SetInfoError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InfoErr = err
}

// SetPingResult sets the result returned by Ping().
func (s *SystemService) SetPingResult(ping *domain.PingResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PingResult = ping
}

// SetPingError sets the error returned by Ping().
func (s *SystemService) SetPingError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PingErr = err
}

// SetVersionResult sets the result returned by Version().
func (s *SystemService) SetVersionResult(version *domain.Version) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.VersionResult = version
}

// SetVersionError sets the error returned by Version().
func (s *SystemService) SetVersionError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.VersionErr = err
}

// SetDiskUsageResult sets the result returned by DiskUsage().
func (s *SystemService) SetDiskUsageResult(usage *domain.DiskUsage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.DiskUsageResult = usage
}

// SetDiskUsageError sets the error returned by DiskUsage().
func (s *SystemService) SetDiskUsageError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.DiskUsageErr = err
}
