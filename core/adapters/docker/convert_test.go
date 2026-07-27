// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	containertypes "github.com/moby/moby/api/types/container"
	networktypes "github.com/moby/moby/api/types/network"

	"github.com/netresearch/ofelia/core/domain"
)

// mockNotFoundError implements cerrdefs.ErrNotFound
type mockNotFoundError struct {
	msg string
}

func (e mockNotFoundError) Error() string        { return e.msg }
func (e mockNotFoundError) NotFound() bool       { return true }
func (e mockNotFoundError) Is(target error) bool { return cerrdefs.IsNotFound(target) }

// mockConflictError implements cerrdefs.ErrConflict
type mockConflictError struct {
	msg string
}

func (e mockConflictError) Error() string        { return e.msg }
func (e mockConflictError) Conflict() bool       { return true }
func (e mockConflictError) Is(target error) bool { return cerrdefs.IsConflict(target) }

// mockUnauthorizedError implements cerrdefs.ErrUnauthorized
type mockUnauthorizedError struct {
	msg string
}

func (e mockUnauthorizedError) Error() string        { return e.msg }
func (e mockUnauthorizedError) Unauthorized() bool   { return true }
func (e mockUnauthorizedError) Is(target error) bool { return cerrdefs.IsUnauthorized(target) }

// mockForbiddenError implements cerrdefs.ErrForbidden
type mockForbiddenError struct {
	msg string
}

func (e mockForbiddenError) Error() string        { return e.msg }
func (e mockForbiddenError) Forbidden() bool      { return true }
func (e mockForbiddenError) Is(target error) bool { return cerrdefs.IsPermissionDenied(target) }

// mockDeadlineError implements cerrdefs.ErrDeadline
type mockDeadlineError struct {
	msg string
}

func (e mockDeadlineError) Error() string          { return e.msg }
func (e mockDeadlineError) DeadlineExceeded() bool { return true }
func (e mockDeadlineError) Is(target error) bool   { return cerrdefs.IsDeadlineExceeded(target) }

// mockCanceledError implements cerrdefs.ErrCancelled.
type mockCanceledError struct {
	msg string
}

func (e mockCanceledError) Error() string        { return e.msg }
func (e mockCanceledError) Cancelled() bool      { return true } //nolint:misspell // Docker SDK uses British spelling
func (e mockCanceledError) Is(target error) bool { return cerrdefs.IsCanceled(target) }

// mockUnavailableError implements cerrdefs.ErrUnavailable
type mockUnavailableError struct {
	msg string
}

func (e mockUnavailableError) Error() string        { return e.msg }
func (e mockUnavailableError) Unavailable() bool    { return true }
func (e mockUnavailableError) Is(target error) bool { return cerrdefs.IsUnavailable(target) }

func TestConvertError(t *testing.T) {
	tests := []struct {
		name        string
		input       error
		wantType    error
		wantMessage string
	}{
		{
			name:     "nil error",
			input:    nil,
			wantType: nil,
		},
		{
			name:        "not found error",
			input:       mockNotFoundError{msg: "container not found"},
			wantType:    &domain.ContainerNotFoundError{},
			wantMessage: "container not found: container not found",
		},
		{
			name:     "conflict error",
			input:    mockConflictError{msg: "name conflict"},
			wantType: domain.ErrConflict,
		},
		{
			name:     "unauthorized error",
			input:    mockUnauthorizedError{msg: "auth failed"},
			wantType: domain.ErrUnauthorized,
		},
		{
			name:     "forbidden error",
			input:    mockForbiddenError{msg: "access denied"},
			wantType: domain.ErrForbidden,
		},
		{
			name:     "deadline error",
			input:    mockDeadlineError{msg: "deadline exceeded"},
			wantType: domain.ErrTimeout,
		},
		{
			name:     "canceled error",
			input:    mockCanceledError{msg: "operation canceled"},
			wantType: domain.ErrCanceled,
		},
		{
			name:     "unavailable error",
			input:    mockUnavailableError{msg: "service unavailable"},
			wantType: domain.ErrConnectionFailed,
		},
		{
			name:        "generic error",
			input:       errors.New("generic error"),
			wantType:    nil,
			wantMessage: "generic error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := convertError(tc.input)

			if tc.wantType == nil && tc.wantMessage == "" {
				if result != nil {
					t.Errorf("convertError() = %v, want nil", result)
				}
				return
			}

			if result == nil {
				t.Fatal("convertError() returned nil, want non-nil error")
			}

			// Check error message if specified
			if tc.wantMessage != "" {
				if result.Error() != tc.wantMessage {
					t.Errorf("convertError() error = %q, want %q", result.Error(), tc.wantMessage)
				}
			}

			// Check error type
			switch expected := tc.wantType.(type) {
			case *domain.ContainerNotFoundError:
				if _, ok := result.(*domain.ContainerNotFoundError); !ok {
					t.Errorf("convertError() type = %T, want *domain.ContainerNotFoundError", result)
				}
			case error:
				if !errors.Is(result, expected) {
					t.Errorf("convertError() = %v, want %v", result, expected)
				}
			}
		})
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantZero bool
		wantTime time.Time
	}{
		{
			name:     "empty string",
			input:    "",
			wantZero: true,
		},
		{
			name:     "valid RFC3339Nano",
			input:    "2024-01-15T10:30:45.123456789Z",
			wantTime: time.Date(2024, 1, 15, 10, 30, 45, 123456789, time.UTC),
		},
		{
			name:     "valid RFC3339",
			input:    "2024-01-15T10:30:45Z",
			wantTime: time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC),
		},
		{
			name:     "invalid format",
			input:    "not-a-time",
			wantZero: true,
		},
		{
			name:     "invalid date",
			input:    "2024-13-45T10:30:45Z",
			wantZero: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseTime(tc.input)

			if tc.wantZero {
				if !result.IsZero() {
					t.Errorf("parseTime(%q) = %v, want zero time", tc.input, result)
				}
				return
			}

			if !result.Equal(tc.wantTime) {
				t.Errorf("parseTime(%q) = %v, want %v", tc.input, result, tc.wantTime)
			}
		})
	}
}

func TestConvertFromContainerJSON(t *testing.T) {
	validTime := "2024-01-15T10:30:45Z"
	startedTime := "2024-01-15T10:31:00Z"
	finishedTime := "2024-01-15T10:35:00Z"

	tests := []struct {
		name  string
		input *containertypes.InspectResponse
		check func(t *testing.T, result *domain.Container)
	}{
		{
			name:  "nil input",
			input: nil,
			check: func(t *testing.T, result *domain.Container) {
				if result != nil {
					t.Errorf("convertFromContainerJSON(nil) = %v, want nil", result)
				}
			},
		},
		{
			name: "basic container",
			input: &containertypes.InspectResponse{
				ID:      "abc123",
				Name:    "/my-container",
				Image:   "sha256:abc123",
				Created: validTime,
				Config: &containertypes.Config{
					Labels: map[string]string{"app": "test"},
				},
			},
			check: func(t *testing.T, result *domain.Container) {
				if result == nil {
					t.Fatal("convertFromContainerJSON() returned nil")
				}
				if result.ID != "abc123" {
					t.Errorf("ID = %q, want %q", result.ID, "abc123")
				}
				if result.Name != "/my-container" {
					t.Errorf("Name = %q, want %q", result.Name, "/my-container")
				}
				if result.Image != "sha256:abc123" {
					t.Errorf("Image = %q, want %q", result.Image, "sha256:abc123")
				}
				if result.Labels == nil || result.Labels["app"] != "test" {
					t.Errorf("Labels = %v, want map[app:test]", result.Labels)
				}
			},
		},
		{
			name: "container with state",
			input: &containertypes.InspectResponse{
				ID:      "def456",
				Name:    "/stateful",
				Created: validTime,
				State: &containertypes.State{
					Running:    true,
					Paused:     false,
					Restarting: false,
					OOMKilled:  false,
					Dead:       false,
					Pid:        12345,
					ExitCode:   0,
					Error:      "",
					StartedAt:  startedTime,
					FinishedAt: finishedTime,
				},
				Config: &containertypes.Config{},
			},
			check: func(t *testing.T, result *domain.Container) {
				if result == nil {
					t.Fatal("convertFromContainerJSON() returned nil")
				}
				if !result.State.Running {
					t.Error("State.Running = false, want true")
				}
				if result.State.Pid != 12345 {
					t.Errorf("State.Pid = %d, want 12345", result.State.Pid)
				}
				if result.State.ExitCode != 0 {
					t.Errorf("State.ExitCode = %d, want 0", result.State.ExitCode)
				}
				expectedStart := parseTime(startedTime)
				if !result.State.StartedAt.Equal(expectedStart) {
					t.Errorf("State.StartedAt = %v, want %v", result.State.StartedAt, expectedStart)
				}
			},
		},
		{
			name: "container with health",
			input: &containertypes.InspectResponse{
				ID:      "ghi789",
				Name:    "/healthy",
				Created: validTime,
				State: &containertypes.State{
					Health: &containertypes.Health{
						Status:        "healthy",
						FailingStreak: 0,
						Log: []*containertypes.HealthcheckResult{
							{
								Start:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
								End:      time.Date(2024, 1, 15, 10, 30, 1, 0, time.UTC),
								ExitCode: 0,
								Output:   "OK",
							},
						},
					},
				},
				Config: &containertypes.Config{},
			},
			check: func(t *testing.T, result *domain.Container) {
				if result == nil {
					t.Fatal("convertFromContainerJSON() returned nil")
				}
				if result.State.Health == nil {
					t.Fatal("State.Health is nil")
				}
				if result.State.Health.Status != "healthy" {
					t.Errorf("State.Health.Status = %q, want %q", result.State.Health.Status, "healthy")
				}
				if result.State.Health.FailingStreak != 0 {
					t.Errorf("State.Health.FailingStreak = %d, want 0", result.State.Health.FailingStreak)
				}
				if len(result.State.Health.Log) != 1 {
					t.Fatalf("len(State.Health.Log) = %d, want 1", len(result.State.Health.Log))
				}
				if result.State.Health.Log[0].Output != "OK" {
					t.Errorf("State.Health.Log[0].Output = %q, want %q", result.State.Health.Log[0].Output, "OK")
				}
			},
		},
		{
			name: "container with config",
			input: &containertypes.InspectResponse{
				ID:      "jkl012",
				Name:    "/configured",
				Created: validTime,
				Config: &containertypes.Config{
					Image:        "nginx:latest",
					Cmd:          []string{"nginx", "-g", "daemon off;"},
					Entrypoint:   []string{"/docker-entrypoint.sh"},
					Env:          []string{"PATH=/usr/local/bin", "ENV=prod"},
					WorkingDir:   "/app",
					User:         "www-data",
					Hostname:     "webserver",
					AttachStdin:  false,
					AttachStdout: true,
					AttachStderr: true,
					Tty:          false,
					OpenStdin:    false,
					StdinOnce:    false,
					Labels:       map[string]string{"version": "1.0"},
				},
			},
			check: func(t *testing.T, result *domain.Container) {
				if result == nil {
					t.Fatal("convertFromContainerJSON() returned nil")
				}
				if result.Config == nil {
					t.Fatal("Config is nil")
				}
				if result.Config.Image != "nginx:latest" {
					t.Errorf("Config.Image = %q, want %q", result.Config.Image, "nginx:latest")
				}
				if len(result.Config.Cmd) != 3 {
					t.Errorf("len(Config.Cmd) = %d, want 3", len(result.Config.Cmd))
				}
				if result.Config.WorkingDir != "/app" {
					t.Errorf("Config.WorkingDir = %q, want %q", result.Config.WorkingDir, "/app")
				}
				if result.Config.User != "www-data" {
					t.Errorf("Config.User = %q, want %q", result.Config.User, "www-data")
				}
				if !result.Config.AttachStdout {
					t.Error("Config.AttachStdout = false, want true")
				}
			},
		},
		{
			name: "container with mounts",
			input: &containertypes.InspectResponse{
				ID:      "mno345",
				Name:    "/mounted",
				Created: validTime,
				Mounts: []containertypes.MountPoint{
					{
						Type:        "bind",
						Source:      "/host/path",
						Destination: "/container/path",
						RW:          true,
					},
					{
						Type:        "volume",
						Source:      "my-volume",
						Destination: "/data",
						RW:          false,
					},
				},
				Config: &containertypes.Config{},
			},
			check: func(t *testing.T, result *domain.Container) {
				if result == nil {
					t.Fatal("convertFromContainerJSON() returned nil")
				}
				if len(result.Mounts) != 2 {
					t.Fatalf("len(Mounts) = %d, want 2", len(result.Mounts))
				}
				if result.Mounts[0].Type != domain.MountTypeBind {
					t.Errorf("Mounts[0].Type = %q, want %q", result.Mounts[0].Type, domain.MountTypeBind)
				}
				if result.Mounts[0].Source != "/host/path" {
					t.Errorf("Mounts[0].Source = %q, want %q", result.Mounts[0].Source, "/host/path")
				}
				if result.Mounts[0].Target != "/container/path" {
					t.Errorf("Mounts[0].Target = %q, want %q", result.Mounts[0].Target, "/container/path")
				}
				if result.Mounts[0].ReadOnly {
					t.Error("Mounts[0].ReadOnly = true, want false")
				}
				if result.Mounts[1].ReadOnly != true {
					t.Error("Mounts[1].ReadOnly = false, want true")
				}
			},
		},
		{
			name: "container with nil state",
			input: &containertypes.InspectResponse{
				ID:      "pqr678",
				Name:    "/no-state",
				Created: validTime,
				State:   nil,
				Config:  &containertypes.Config{},
			},
			check: func(t *testing.T, result *domain.Container) {
				if result == nil {
					t.Fatal("convertFromContainerJSON() returned nil")
				}
				// State should have default values
				if result.State.Running {
					t.Error("State.Running = true, want false (default)")
				}
			},
		},
		{
			name: "container with empty config",
			input: &containertypes.InspectResponse{
				ID:      "stu901",
				Name:    "/empty-config",
				Created: validTime,
				Config:  &containertypes.Config{},
			},
			check: func(t *testing.T, result *domain.Container) {
				if result == nil {
					t.Fatal("convertFromContainerJSON() returned nil")
				}
				if result.Config == nil {
					t.Error("Config is nil, want non-nil empty config")
				}
				if result.Config != nil && result.Config.Image != "" {
					t.Errorf("Config.Image = %q, want empty", result.Config.Image)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := convertFromContainerJSON(tc.input)
			tc.check(t, result)
		})
	}
}

func TestConvertFromAPIContainer(t *testing.T) {
	tests := []struct {
		name  string
		input *containertypes.Summary
		check func(t *testing.T, result domain.Container)
	}{
		{
			name: "basic container",
			input: &containertypes.Summary{
				ID:      "abc123",
				Names:   []string{"/my-container"},
				Image:   "nginx:latest",
				Created: 1705315845, // Unix timestamp
				State:   "running",
				Labels:  map[string]string{"env": "prod"},
			},
			check: func(t *testing.T, result domain.Container) {
				if result.ID != "abc123" {
					t.Errorf("ID = %q, want %q", result.ID, "abc123")
				}
				// Regression test for issue #422: Docker API returns names with leading slash
				// but Ofelia should strip it to prevent malformed API URLs like /api/jobs//container.job/history
				if result.Name != "my-container" {
					t.Errorf("Name = %q, want %q (leading slash should be stripped)", result.Name, "my-container")
				}
				if result.Image != "nginx:latest" {
					t.Errorf("Image = %q, want %q", result.Image, "nginx:latest")
				}
				if !result.State.Running {
					t.Error("State.Running = false, want true")
				}
				expectedTime := time.Unix(1705315845, 0)
				if !result.Created.Equal(expectedTime) {
					t.Errorf("Created = %v, want %v", result.Created, expectedTime)
				}
				if result.Labels["env"] != "prod" {
					t.Errorf("Labels[env] = %q, want %q", result.Labels["env"], "prod")
				}
			},
		},
		{
			name: "stopped container",
			input: &containertypes.Summary{
				ID:      "def456",
				Names:   []string{"/stopped"},
				Image:   "alpine:latest",
				Created: 1705315845,
				State:   "exited",
			},
			check: func(t *testing.T, result domain.Container) {
				if result.State.Running {
					t.Error("State.Running = true, want false")
				}
				// Leading slash should be stripped (issue #422)
				if result.Name != "stopped" {
					t.Errorf("Name = %q, want %q (leading slash stripped)", result.Name, "stopped")
				}
			},
		},
		{
			name: "container with no names",
			input: &containertypes.Summary{
				ID:      "ghi789",
				Names:   []string{},
				Image:   "busybox:latest",
				Created: 1705315845,
				State:   "running",
			},
			check: func(t *testing.T, result domain.Container) {
				if result.Name != "" {
					t.Errorf("Name = %q, want empty string", result.Name)
				}
			},
		},
		{
			name: "container with multiple names",
			input: &containertypes.Summary{
				ID:      "jkl012",
				Names:   []string{"/primary", "/alias"},
				Image:   "redis:latest",
				Created: 1705315845,
				State:   "running",
			},
			check: func(t *testing.T, result domain.Container) {
				// First name should be used with leading slash stripped (issue #422)
				if result.Name != "primary" {
					t.Errorf("Name = %q, want %q (first name, leading slash stripped)", result.Name, "primary")
				}
			},
		},
		{
			name: "container with nil labels",
			input: &containertypes.Summary{
				ID:      "mno345",
				Names:   []string{"/no-labels"},
				Image:   "postgres:latest",
				Created: 1705315845,
				State:   "running",
				Labels:  nil,
			},
			check: func(t *testing.T, result domain.Container) {
				if result.Labels != nil {
					t.Errorf("Labels = %v, want nil", result.Labels)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := convertFromAPIContainer(tc.input)
			tc.check(t, result)
		})
	}
}

func TestConvertFromNetworkResource(t *testing.T) {
	validTime := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)

	tests := []struct {
		name  string
		input *networktypes.Summary
		check func(t *testing.T, result domain.Network)
	}{
		{
			name: "basic network",
			input: &networktypes.Summary{
				Network: networktypes.Network{
					Name:       "my-network",
					ID:         "net123",
					Created:    validTime,
					Scope:      "local",
					Driver:     "bridge",
					EnableIPv6: false,
					Internal:   false,
					Attachable: true,
					Ingress:    false,
					Options:    map[string]string{"com.docker.network.bridge.name": "docker0"},
					Labels:     map[string]string{"env": "test"},
				},
			},
			check: func(t *testing.T, result domain.Network) {
				if result.Name != "my-network" {
					t.Errorf("Name = %q, want %q", result.Name, "my-network")
				}
				if result.ID != "net123" {
					t.Errorf("ID = %q, want %q", result.ID, "net123")
				}
				if !result.Created.Equal(validTime) {
					t.Errorf("Created = %v, want %v", result.Created, validTime)
				}
				if result.Scope != "local" {
					t.Errorf("Scope = %q, want %q", result.Scope, "local")
				}
				if result.Driver != "bridge" {
					t.Errorf("Driver = %q, want %q", result.Driver, "bridge")
				}
				if !result.Attachable {
					t.Error("Attachable = false, want true")
				}
			},
		},
		{
			name: "network with IPAM",
			input: &networktypes.Summary{
				Network: networktypes.Network{
					Name:    "ipam-network",
					ID:      "net456",
					Created: validTime,
					Driver:  "bridge",
					IPAM: networktypes.IPAM{
						Driver: "default",
						Options: map[string]string{
							"option1": "value1",
						},
						Config: []networktypes.IPAMConfig{
							{
								Subnet:  netip.MustParsePrefix("172.20.0.0/16"),
								IPRange: netip.MustParsePrefix("172.20.10.0/24"),
								Gateway: netip.MustParseAddr("172.20.0.1"),
								AuxAddress: map[string]netip.Addr{
									"host1": netip.MustParseAddr("172.20.0.2"),
								},
							},
						},
					},
				},
			},
			check: func(t *testing.T, result domain.Network) {
				if result.IPAM.Driver != "default" {
					t.Errorf("IPAM.Driver = %q, want %q", result.IPAM.Driver, "default")
				}
				if len(result.IPAM.Config) != 1 {
					t.Fatalf("len(IPAM.Config) = %d, want 1", len(result.IPAM.Config))
				}
				if result.IPAM.Config[0].Subnet != "172.20.0.0/16" {
					t.Errorf("IPAM.Config[0].Subnet = %q, want %q", result.IPAM.Config[0].Subnet, "172.20.0.0/16")
				}
				if result.IPAM.Config[0].Gateway != "172.20.0.1" {
					t.Errorf("IPAM.Config[0].Gateway = %q, want %q", result.IPAM.Config[0].Gateway, "172.20.0.1")
				}
			},
		},
		{
			name: "network with empty IPAM",
			input: &networktypes.Summary{
				Network: networktypes.Network{
					Name:    "no-ipam",
					ID:      "net012",
					Created: validTime,
					Driver:  "bridge",
					IPAM: networktypes.IPAM{
						Driver: "",
						Config: []networktypes.IPAMConfig{},
					},
				},
			},
			check: func(t *testing.T, result domain.Network) {
				if result.IPAM.Driver != "" {
					t.Errorf("IPAM.Driver = %q, want empty", result.IPAM.Driver)
				}
				if len(result.IPAM.Config) != 0 {
					t.Errorf("len(IPAM.Config) = %d, want 0", len(result.IPAM.Config))
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := convertFromNetworkResource(tc.input)
			tc.check(t, result)
		})
	}
}

func TestConvertFromNetworkInspect(t *testing.T) {
	validTime := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)

	tests := []struct {
		name  string
		input *networktypes.Inspect
		check func(t *testing.T, result *domain.Network)
	}{
		{
			name: "basic network",
			input: &networktypes.Inspect{
				Network: networktypes.Network{
					Name:       "inspect-network",
					ID:         "net123",
					Created:    validTime,
					Scope:      "local",
					Driver:     "bridge",
					EnableIPv6: true,
					Internal:   false,
					Attachable: true,
					Ingress:    false,
					Options:    map[string]string{"mtu": "1500"},
					Labels:     map[string]string{"owner": "admin"},
				},
			},
			check: func(t *testing.T, result *domain.Network) {
				if result == nil {
					t.Fatal("convertFromNetworkInspect() returned nil")
				}
				if result.Name != "inspect-network" {
					t.Errorf("Name = %q, want %q", result.Name, "inspect-network")
				}
				if !result.EnableIPv6 {
					t.Error("EnableIPv6 = false, want true")
				}
				if result.Options["mtu"] != "1500" {
					t.Errorf("Options[mtu] = %q, want %q", result.Options["mtu"], "1500")
				}
			},
		},
		{
			name: "network with IPAM",
			input: &networktypes.Inspect{
				Network: networktypes.Network{
					Name:    "ipam-inspect",
					ID:      "net456",
					Created: validTime,
					Driver:  "overlay",
					IPAM: networktypes.IPAM{
						Driver: "default",
						Options: map[string]string{
							"subnet": "custom",
						},
						Config: []networktypes.IPAMConfig{
							{
								Subnet:  netip.MustParsePrefix("10.0.0.0/8"),
								IPRange: netip.MustParsePrefix("10.0.1.0/24"),
								Gateway: netip.MustParseAddr("10.0.0.1"),
							},
							{
								Subnet:  netip.MustParsePrefix("fd00::/64"),
								Gateway: netip.MustParseAddr("fd00::1"),
							},
						},
					},
				},
			},
			check: func(t *testing.T, result *domain.Network) {
				if result == nil {
					t.Fatal("convertFromNetworkInspect() returned nil")
				}
				if result.IPAM.Driver != "default" {
					t.Errorf("IPAM.Driver = %q, want %q", result.IPAM.Driver, "default")
				}
				if len(result.IPAM.Config) != 2 {
					t.Fatalf("len(IPAM.Config) = %d, want 2", len(result.IPAM.Config))
				}
				if result.IPAM.Config[1].Subnet != "fd00::/64" {
					t.Errorf("IPAM.Config[1].Subnet = %q, want %q", result.IPAM.Config[1].Subnet, "fd00::/64")
				}
			},
		},
		{
			name: "network with containers",
			input: &networktypes.Inspect{
				Network: networktypes.Network{
					Name:    "inspect-containers",
					ID:      "net789",
					Created: validTime,
					Driver:  "bridge",
				},
				Containers: map[string]networktypes.EndpointResource{
					"c1": {
						Name:        "app",
						EndpointID:  "endpoint1",
						MacAddress:  mustMAC("00:11:22:33:44:55"),
						IPv4Address: netip.MustParsePrefix("192.168.1.10/24"),
					},
					"c2": {
						Name:        "db",
						EndpointID:  "endpoint2",
						MacAddress:  mustMAC("02:42:ac:11:00:03"),
						IPv4Address: netip.MustParsePrefix("172.17.0.3/16"),
						IPv6Address: netip.MustParsePrefix("fe80::42:acff:fe11:3/64"),
					},
				},
			},
			check: func(t *testing.T, result *domain.Network) {
				if result == nil {
					t.Fatal("convertFromNetworkInspect() returned nil")
				}
				if len(result.Containers) != 2 {
					t.Fatalf("len(Containers) = %d, want 2", len(result.Containers))
				}
				app, ok := result.Containers["c1"]
				if !ok {
					t.Fatal("c1 not found in Containers")
				}
				if app.Name != "app" {
					t.Errorf("Containers[c1].Name = %q, want %q", app.Name, "app")
				}
				if app.IPv4Address != "192.168.1.10/24" {
					t.Errorf("Containers[c1].IPv4Address = %q, want %q", app.IPv4Address, "192.168.1.10/24")
				}
				if app.IPv6Address != "" {
					t.Errorf("Containers[c1].IPv6Address = %q, want empty", app.IPv6Address)
				}
				db := result.Containers["c2"]
				if db.IPv6Address != "fe80::42:acff:fe11:3/64" {
					t.Errorf("Containers[c2].IPv6Address = %q, want %q", db.IPv6Address, "fe80::42:acff:fe11:3/64")
				}
				if app.MacAddress != "00:11:22:33:44:55" {
					t.Errorf("Containers[c1].MacAddress = %q, want %q", app.MacAddress, "00:11:22:33:44:55")
				}
			},
		},
		{
			name: "network with only driver in IPAM",
			input: &networktypes.Inspect{
				Network: networktypes.Network{
					Name:    "driver-only-ipam",
					ID:      "net012",
					Created: validTime,
					Driver:  "bridge",
					IPAM: networktypes.IPAM{
						Driver: "custom-driver",
						Config: []networktypes.IPAMConfig{},
					},
				},
			},
			check: func(t *testing.T, result *domain.Network) {
				if result == nil {
					t.Fatal("convertFromNetworkInspect() returned nil")
				}
				if result.IPAM.Driver != "custom-driver" {
					t.Errorf("IPAM.Driver = %q, want %q", result.IPAM.Driver, "custom-driver")
				}
				if len(result.IPAM.Config) != 0 {
					t.Errorf("len(IPAM.Config) = %d, want 0", len(result.IPAM.Config))
				}
			},
		},
		{
			name: "network with only config in IPAM",
			input: &networktypes.Inspect{
				Network: networktypes.Network{
					Name:    "config-only-ipam",
					ID:      "net345",
					Created: validTime,
					Driver:  "bridge",
					IPAM: networktypes.IPAM{
						Driver: "",
						Config: []networktypes.IPAMConfig{
							{
								Subnet: netip.MustParsePrefix("172.30.0.0/16"),
							},
						},
					},
				},
			},
			check: func(t *testing.T, result *domain.Network) {
				if result == nil {
					t.Fatal("convertFromNetworkInspect() returned nil")
				}
				if result.IPAM.Driver != "" {
					t.Errorf("IPAM.Driver = %q, want empty", result.IPAM.Driver)
				}
				if len(result.IPAM.Config) != 1 {
					t.Fatalf("len(IPAM.Config) = %d, want 1", len(result.IPAM.Config))
				}
				if result.IPAM.Config[0].Subnet != "172.30.0.0/16" {
					t.Errorf("IPAM.Config[0].Subnet = %q, want %q", result.IPAM.Config[0].Subnet, "172.30.0.0/16")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := convertFromNetworkInspect(tc.input)
			tc.check(t, result)
		})
	}
}
