// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"net/netip"
	"os"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/domain"
)

// --- convertToContainerConfig ---

func TestConvertToContainerConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    *domain.ContainerConfig
		validate func(t *testing.T, result *container.Config)
	}{
		{
			name:  "nil input returns nil",
			input: nil,
			validate: func(t *testing.T, result *container.Config) {
				assert.Nil(t, result)
			},
		},
		{
			name:  "empty config returns empty",
			input: &domain.ContainerConfig{},
			validate: func(t *testing.T, result *container.Config) {
				require.NotNil(t, result)
				assert.Empty(t, result.Hostname)
				assert.Empty(t, result.Image)
			},
		},
		{
			name: "all fields mapped",
			input: &domain.ContainerConfig{
				Hostname:     "myhost",
				User:         "root",
				AttachStdin:  true,
				AttachStdout: true,
				AttachStderr: true,
				Tty:          true,
				OpenStdin:    true,
				StdinOnce:    true,
				Env:          []string{"FOO=bar", "BAZ=qux"},
				Cmd:          []string{"echo", "hello"},
				Image:        "alpine:latest",
				WorkingDir:   "/app",
				Entrypoint:   []string{"/bin/sh"},
				Labels:       map[string]string{"app": "test"},
			},
			validate: func(t *testing.T, result *container.Config) {
				require.NotNil(t, result)
				assert.Equal(t, "myhost", result.Hostname)
				assert.Equal(t, "root", result.User)
				assert.True(t, result.AttachStdin)
				assert.True(t, result.AttachStdout)
				assert.True(t, result.AttachStderr)
				assert.True(t, result.Tty)
				assert.True(t, result.OpenStdin)
				assert.True(t, result.StdinOnce)
				assert.Equal(t, []string{"FOO=bar", "BAZ=qux"}, result.Env)
				assert.Equal(t, []string{"echo", "hello"}, result.Cmd)
				assert.Equal(t, "alpine:latest", result.Image)
				assert.Equal(t, "/app", result.WorkingDir)
				assert.Equal(t, []string{"/bin/sh"}, result.Entrypoint)
				assert.Equal(t, map[string]string{"app": "test"}, result.Labels)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertToContainerConfig(tt.input)
			tt.validate(t, result)
		})
	}
}

// --- convertToHostConfig ---

func TestConvertToHostConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    *domain.HostConfig
		validate func(t *testing.T, result *container.HostConfig)
	}{
		{
			name:  "nil input returns nil",
			input: nil,
			validate: func(t *testing.T, result *container.HostConfig) {
				assert.Nil(t, result)
			},
		},
		{
			name: "binds and network mode",
			input: &domain.HostConfig{
				Binds:       []string{"/host:/container:ro"},
				NetworkMode: "bridge",
			},
			validate: func(t *testing.T, result *container.HostConfig) {
				require.NotNil(t, result)
				assert.Equal(t, []string{"/host:/container:ro"}, result.Binds)
				assert.Equal(t, container.NetworkMode("bridge"), result.NetworkMode)
			},
		},
		{
			name: "port bindings",
			input: &domain.HostConfig{
				PortBindings: domain.PortMap{
					"80/tcp": {{HostIP: "0.0.0.0", HostPort: "8080"}},
				},
			},
			validate: func(t *testing.T, result *container.HostConfig) {
				require.NotNil(t, result)
				bindings, ok := result.PortBindings[network.MustParsePort("80/tcp")]
				require.True(t, ok)
				require.Len(t, bindings, 1)
				assert.Equal(t, netip.MustParseAddr("0.0.0.0"), bindings[0].HostIP)
				assert.Equal(t, "8080", bindings[0].HostPort)
			},
		},
		{
			name: "mounts with bind options",
			input: &domain.HostConfig{
				Mounts: []domain.Mount{
					{
						Type:     domain.MountTypeBind,
						Source:   "/src",
						Target:   "/dst",
						ReadOnly: true,
						BindOptions: &domain.BindOptions{
							Propagation: "rprivate",
						},
					},
				},
			},
			validate: func(t *testing.T, result *container.HostConfig) {
				require.NotNil(t, result)
				require.Len(t, result.Mounts, 1)
				m := result.Mounts[0]
				assert.Equal(t, mount.TypeBind, m.Type)
				assert.Equal(t, "/src", m.Source)
				assert.Equal(t, "/dst", m.Target)
				assert.True(t, m.ReadOnly)
				require.NotNil(t, m.BindOptions)
				assert.Equal(t, mount.Propagation("rprivate"), m.BindOptions.Propagation)
			},
		},
		{
			name: "ulimits",
			input: &domain.HostConfig{
				Ulimits: []domain.Ulimit{
					{Name: "nofile", Soft: 1024, Hard: 2048},
					{Name: "nproc", Soft: 512, Hard: 1024},
				},
			},
			validate: func(t *testing.T, result *container.HostConfig) {
				require.NotNil(t, result)
				require.Len(t, result.Ulimits, 2)
				assert.Equal(t, "nofile", result.Ulimits[0].Name)
				assert.Equal(t, int64(1024), result.Ulimits[0].Soft)
				assert.Equal(t, int64(2048), result.Ulimits[0].Hard)
				assert.Equal(t, "nproc", result.Ulimits[1].Name)
			},
		},
		{
			name: "resource limits",
			input: &domain.HostConfig{
				Memory:     536870912,
				MemorySwap: 1073741824,
				CPUShares:  512,
				CPUPeriod:  100000,
				CPUQuota:   50000,
				NanoCPUs:   1500000000,
			},
			validate: func(t *testing.T, result *container.HostConfig) {
				require.NotNil(t, result)
				assert.Equal(t, int64(536870912), result.Resources.Memory)
				assert.Equal(t, int64(1073741824), result.Resources.MemorySwap)
				assert.Equal(t, int64(512), result.Resources.CPUShares)
				assert.Equal(t, int64(100000), result.Resources.CPUPeriod)
				assert.Equal(t, int64(50000), result.Resources.CPUQuota)
				assert.Equal(t, int64(1500000000), result.Resources.NanoCPUs)
			},
		},
		{
			name: "security options",
			input: &domain.HostConfig{
				AutoRemove:     true,
				Privileged:     true,
				ReadonlyRootfs: true,
				DNS:            []string{"8.8.8.8"},
				DNSSearch:      []string{"example.com"},
				ExtraHosts:     []string{"myhost:192.168.1.1"},
				CapAdd:         []string{"NET_ADMIN"},
				CapDrop:        []string{"MKNOD"},
				SecurityOpt:    []string{"no-new-privileges"},
				PidMode:        "host",
				UsernsMode:     "host",
				ShmSize:        67108864,
				Tmpfs:          map[string]string{"/tmp": "rw,size=64m"},
			},
			validate: func(t *testing.T, result *container.HostConfig) {
				require.NotNil(t, result)
				assert.True(t, result.AutoRemove)
				assert.True(t, result.Privileged)
				assert.True(t, result.ReadonlyRootfs)
				assert.Equal(t, []netip.Addr{netip.MustParseAddr("8.8.8.8")}, result.DNS)
				assert.Equal(t, []string{"example.com"}, result.DNSSearch)
				assert.Equal(t, []string{"myhost:192.168.1.1"}, result.ExtraHosts)
				assert.Contains(t, result.CapAdd, "NET_ADMIN")
				assert.Contains(t, result.CapDrop, "MKNOD")
				assert.Equal(t, []string{"no-new-privileges"}, result.SecurityOpt)
				assert.Equal(t, container.PidMode("host"), result.PidMode)
				assert.Equal(t, container.UsernsMode("host"), result.UsernsMode)
				assert.Equal(t, int64(67108864), result.ShmSize)
				assert.Equal(t, map[string]string{"/tmp": "rw,size=64m"}, result.Tmpfs)
			},
		},
		{
			name: "restart policy and log config",
			input: &domain.HostConfig{
				RestartPolicy: domain.RestartPolicy{
					Name:              "on-failure",
					MaximumRetryCount: 5,
				},
				LogConfig: domain.LogConfig{
					Type:   "json-file",
					Config: map[string]string{"max-size": "10m"},
				},
			},
			validate: func(t *testing.T, result *container.HostConfig) {
				require.NotNil(t, result)
				assert.Equal(t, container.RestartPolicyMode("on-failure"), result.RestartPolicy.Name)
				assert.Equal(t, 5, result.RestartPolicy.MaximumRetryCount)
				assert.Equal(t, "json-file", result.LogConfig.Type)
				assert.Equal(t, map[string]string{"max-size": "10m"}, result.LogConfig.Config)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertToHostConfig(tt.input)
			tt.validate(t, result)
		})
	}
}

// --- convertToNetworkingConfig ---

func TestConvertToNetworkingConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    *domain.NetworkConfig
		validate func(t *testing.T, result *network.NetworkingConfig)
	}{
		{
			name:  "nil input returns nil",
			input: nil,
			validate: func(t *testing.T, result *network.NetworkingConfig) {
				assert.Nil(t, result)
			},
		},
		{
			name: "empty endpoints config",
			input: &domain.NetworkConfig{
				EndpointsConfig: map[string]*domain.EndpointSettings{},
			},
			validate: func(t *testing.T, result *network.NetworkingConfig) {
				require.NotNil(t, result)
				assert.Empty(t, result.EndpointsConfig)
			},
		},
		{
			name: "with endpoints",
			input: &domain.NetworkConfig{
				EndpointsConfig: map[string]*domain.EndpointSettings{
					"my-network": {
						NetworkID: "net-123",
						Aliases:   []string{"web", "frontend"},
						Links:     []string{"db:database"},
					},
				},
			},
			validate: func(t *testing.T, result *network.NetworkingConfig) {
				require.NotNil(t, result)
				ep, ok := result.EndpointsConfig["my-network"]
				require.True(t, ok)
				assert.Equal(t, "net-123", ep.NetworkID)
				assert.Equal(t, []string{"web", "frontend"}, ep.Aliases)
				assert.Equal(t, []string{"db:database"}, ep.Links)
			},
		},
		{
			name: "nil endpoint settings value",
			input: &domain.NetworkConfig{
				EndpointsConfig: map[string]*domain.EndpointSettings{
					"empty-net": nil,
				},
			},
			validate: func(t *testing.T, result *network.NetworkingConfig) {
				require.NotNil(t, result)
				assert.Nil(t, result.EndpointsConfig["empty-net"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertToNetworkingConfig(tt.input)
			tt.validate(t, result)
		})
	}
}

// --- convertToEndpointSettings ---

func TestConvertToEndpointSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    *domain.EndpointSettings
		validate func(t *testing.T, result *network.EndpointSettings)
	}{
		{
			name:  "nil input returns nil",
			input: nil,
			validate: func(t *testing.T, result *network.EndpointSettings) {
				assert.Nil(t, result)
			},
		},
		{
			name: "all fields with IPAM",
			input: &domain.EndpointSettings{
				Links:               []string{"db:database"},
				Aliases:             []string{"web"},
				NetworkID:           "net-abc",
				EndpointID:          "ep-123",
				Gateway:             "172.17.0.1",
				IPAddress:           "172.17.0.5",
				IPPrefixLen:         16,
				IPv6Gateway:         "fe80::1",
				GlobalIPv6Address:   "2001:db8::5",
				GlobalIPv6PrefixLen: 64,
				MacAddress:          "02:42:ac:11:00:05",
				DriverOpts:          map[string]string{"opt1": "val1"},
				IPAMConfig: &domain.EndpointIPAMConfig{
					IPv4Address:  "172.17.0.10",
					IPv6Address:  "2001:db8::10",
					LinkLocalIPs: []string{"169.254.0.1"},
				},
			},
			validate: func(t *testing.T, result *network.EndpointSettings) {
				require.NotNil(t, result)
				assert.Equal(t, []string{"db:database"}, result.Links)
				assert.Equal(t, []string{"web"}, result.Aliases)
				assert.Equal(t, "net-abc", result.NetworkID)
				assert.Equal(t, "ep-123", result.EndpointID)
				assert.Equal(t, netip.MustParseAddr("172.17.0.1"), result.Gateway)
				assert.Equal(t, netip.MustParseAddr("172.17.0.5"), result.IPAddress)
				assert.Equal(t, 16, result.IPPrefixLen)
				assert.Equal(t, netip.MustParseAddr("fe80::1"), result.IPv6Gateway)
				assert.Equal(t, netip.MustParseAddr("2001:db8::5"), result.GlobalIPv6Address)
				assert.Equal(t, 64, result.GlobalIPv6PrefixLen)
				assert.Equal(t, "02:42:ac:11:00:05", result.MacAddress.String())
				assert.Equal(t, map[string]string{"opt1": "val1"}, result.DriverOpts)
				require.NotNil(t, result.IPAMConfig)
				assert.Equal(t, netip.MustParseAddr("172.17.0.10"), result.IPAMConfig.IPv4Address)
				assert.Equal(t, netip.MustParseAddr("2001:db8::10"), result.IPAMConfig.IPv6Address)
				assert.Equal(t, []netip.Addr{netip.MustParseAddr("169.254.0.1")}, result.IPAMConfig.LinkLocalIPs)
			},
		},
		{
			name: "without IPAM config",
			input: &domain.EndpointSettings{
				NetworkID: "net-def",
				Aliases:   []string{"svc"},
			},
			validate: func(t *testing.T, result *network.EndpointSettings) {
				require.NotNil(t, result)
				assert.Equal(t, "net-def", result.NetworkID)
				assert.Nil(t, result.IPAMConfig)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertToEndpointSettings(tt.input)
			tt.validate(t, result)
		})
	}
}

// --- convertToPortMap ---

func TestConvertToPortMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    domain.PortMap
		validate func(t *testing.T, result network.PortMap)
	}{
		{
			name:  "nil input returns nil",
			input: nil,
			validate: func(t *testing.T, result network.PortMap) {
				assert.Nil(t, result)
			},
		},
		{
			name:  "empty map returns nil",
			input: domain.PortMap{},
			validate: func(t *testing.T, result network.PortMap) {
				assert.Nil(t, result)
			},
		},
		{
			name: "single port single binding",
			input: domain.PortMap{
				"80/tcp": {{HostIP: "0.0.0.0", HostPort: "8080"}},
			},
			validate: func(t *testing.T, result network.PortMap) {
				require.NotNil(t, result)
				bindings := result[network.MustParsePort("80/tcp")]
				require.Len(t, bindings, 1)
				assert.Equal(t, netip.MustParseAddr("0.0.0.0"), bindings[0].HostIP)
				assert.Equal(t, "8080", bindings[0].HostPort)
			},
		},
		{
			name: "single port multiple bindings",
			input: domain.PortMap{
				"443/tcp": {
					{HostIP: "0.0.0.0", HostPort: "443"},
					{HostIP: "0.0.0.0", HostPort: "8443"},
				},
			},
			validate: func(t *testing.T, result network.PortMap) {
				require.NotNil(t, result)
				bindings := result[network.MustParsePort("443/tcp")]
				require.Len(t, bindings, 2)
			},
		},
		{
			name: "multiple ports",
			input: domain.PortMap{
				"80/tcp":   {{HostIP: "", HostPort: "8080"}},
				"443/tcp":  {{HostIP: "", HostPort: "8443"}},
				"3000/tcp": {{HostIP: "127.0.0.1", HostPort: "3000"}},
			},
			validate: func(t *testing.T, result network.PortMap) {
				require.NotNil(t, result)
				assert.Len(t, result, 3)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertToPortMap(tt.input)
			tt.validate(t, result)
		})
	}
}

// --- convertToMount ---

func TestConvertToMount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    *domain.Mount
		validate func(t *testing.T, result mount.Mount)
	}{
		{
			name: "bind mount with propagation",
			input: &domain.Mount{
				Type:        domain.MountTypeBind,
				Source:      "/host/data",
				Target:      "/container/data",
				ReadOnly:    true,
				Consistency: "consistent",
				BindOptions: &domain.BindOptions{
					Propagation: "shared",
				},
			},
			validate: func(t *testing.T, result mount.Mount) {
				assert.Equal(t, mount.TypeBind, result.Type)
				assert.Equal(t, "/host/data", result.Source)
				assert.Equal(t, "/container/data", result.Target)
				assert.True(t, result.ReadOnly)
				assert.Equal(t, mount.Consistency("consistent"), result.Consistency)
				require.NotNil(t, result.BindOptions)
				assert.Equal(t, mount.Propagation("shared"), result.BindOptions.Propagation)
				assert.Nil(t, result.VolumeOptions)
				assert.Nil(t, result.TmpfsOptions)
			},
		},
		{
			name: "volume mount with driver config",
			input: &domain.Mount{
				Type:   domain.MountTypeVolume,
				Source: "my-volume",
				Target: "/data",
				VolumeOptions: &domain.VolumeOptions{
					NoCopy: true,
					Labels: map[string]string{"env": "prod"},
					DriverConfig: &domain.Driver{
						Name:    "local",
						Options: map[string]string{"type": "nfs"},
					},
				},
			},
			validate: func(t *testing.T, result mount.Mount) {
				assert.Equal(t, mount.TypeVolume, result.Type)
				assert.Equal(t, "my-volume", result.Source)
				require.NotNil(t, result.VolumeOptions)
				assert.True(t, result.VolumeOptions.NoCopy)
				assert.Equal(t, map[string]string{"env": "prod"}, result.VolumeOptions.Labels)
				require.NotNil(t, result.VolumeOptions.DriverConfig)
				assert.Equal(t, "local", result.VolumeOptions.DriverConfig.Name)
				assert.Equal(t, map[string]string{"type": "nfs"}, result.VolumeOptions.DriverConfig.Options)
			},
		},
		{
			name: "volume mount without driver config",
			input: &domain.Mount{
				Type:   domain.MountTypeVolume,
				Source: "my-volume",
				Target: "/data",
				VolumeOptions: &domain.VolumeOptions{
					NoCopy: false,
					Labels: map[string]string{"tier": "web"},
				},
			},
			validate: func(t *testing.T, result mount.Mount) {
				require.NotNil(t, result.VolumeOptions)
				assert.Nil(t, result.VolumeOptions.DriverConfig)
			},
		},
		{
			name: "tmpfs mount",
			input: &domain.Mount{
				Type:   domain.MountTypeTmpfs,
				Target: "/tmp",
				TmpfsOptions: &domain.TmpfsOptions{
					SizeBytes: 67108864,
					Mode:      0o1777,
				},
			},
			validate: func(t *testing.T, result mount.Mount) {
				assert.Equal(t, mount.TypeTmpfs, result.Type)
				assert.Equal(t, "/tmp", result.Target)
				require.NotNil(t, result.TmpfsOptions)
				assert.Equal(t, int64(67108864), result.TmpfsOptions.SizeBytes)
				assert.Equal(t, os.FileMode(0o1777), result.TmpfsOptions.Mode)
			},
		},
		{
			name: "minimal mount without options",
			input: &domain.Mount{
				Type:   domain.MountTypeBind,
				Source: "/src",
				Target: "/dst",
			},
			validate: func(t *testing.T, result mount.Mount) {
				assert.Equal(t, mount.TypeBind, result.Type)
				assert.Nil(t, result.BindOptions)
				assert.Nil(t, result.VolumeOptions)
				assert.Nil(t, result.TmpfsOptions)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertToMount(tt.input)
			tt.validate(t, result)
		})
	}
}
