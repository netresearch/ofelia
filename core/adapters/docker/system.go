package docker

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"

	"github.com/netresearch/ofelia/core/domain"
)

// SystemServiceAdapter implements ports.SystemService using Docker SDK.
type SystemServiceAdapter struct {
	client *client.Client
}

// Info returns system information.
func (s *SystemServiceAdapter) Info(ctx context.Context) (*domain.SystemInfo, error) {
	info, err := s.client.Info(ctx)
	if err != nil {
		return nil, convertError(err)
	}

	domainInfo := &domain.SystemInfo{
		ID:                info.ID,
		Containers:        info.Containers,
		ContainersRunning: info.ContainersRunning,
		ContainersPaused:  info.ContainersPaused,
		ContainersStopped: info.ContainersStopped,
		Images:            info.Images,
		Driver:            info.Driver,
		MemoryLimit:       info.MemoryLimit,
		SwapLimit:         info.SwapLimit,
		KernelMemory:      info.KernelMemory,
		CPUCfsPeriod:      info.CPUCfsPeriod,
		CPUCfsQuota:       info.CPUCfsQuota,
		CPUShares:         info.CPUShares,
		CPUSet:            info.CPUSet,
		PidsLimit:         info.PidsLimit,
		IPv4Forwarding:    info.IPv4Forwarding,
		// BridgeNfIptables and BridgeNfIP6tables are deprecated in official SDK
		Debug:              info.Debug,
		NFd:                info.NFd,
		OomKillDisable:     info.OomKillDisable,
		NGoroutines:        info.NGoroutines,
		SystemTime:         info.SystemTime,
		LoggingDriver:      info.LoggingDriver,
		CgroupDriver:       info.CgroupDriver,
		CgroupVersion:      info.CgroupVersion,
		NEventsListener:    info.NEventsListener,
		KernelVersion:      info.KernelVersion,
		OperatingSystem:    info.OperatingSystem,
		OSVersion:          info.OSVersion,
		OSType:             info.OSType,
		Architecture:       info.Architecture,
		IndexServerAddress: info.IndexServerAddress,
		NCPU:               info.NCPU,
		MemTotal:           info.MemTotal,
		DockerRootDir:      info.DockerRootDir,
		HTTPProxy:          info.HTTPProxy,
		HTTPSProxy:         info.HTTPSProxy,
		NoProxy:            info.NoProxy,
		Name:               info.Name,
		Labels:             info.Labels,
		ExperimentalBuild:  info.ExperimentalBuild,
		ServerVersion:      info.ServerVersion,
		DefaultRuntime:     info.DefaultRuntime,
		LiveRestoreEnabled: info.LiveRestoreEnabled,
		Isolation:          string(info.Isolation),
		InitBinary:         info.InitBinary,
		SecurityOptions:    info.SecurityOptions,
		Warnings:           info.Warnings,
	}

	// Convert driver status
	for _, ds := range info.DriverStatus {
		domainInfo.DriverStatus = append(domainInfo.DriverStatus, [2]string{ds[0], ds[1]})
	}

	// Convert system status
	for _, ss := range info.SystemStatus {
		domainInfo.SystemStatus = append(domainInfo.SystemStatus, [2]string{ss[0], ss[1]})
	}

	// Convert runtimes
	if len(info.Runtimes) > 0 {
		domainInfo.Runtimes = make(map[string]domain.Runtime)
		for name, rt := range info.Runtimes {
			domainInfo.Runtimes[name] = domain.Runtime{
				Path: rt.Path,
				Args: rt.Args,
			}
		}
	}

	// Convert swarm info
	domainInfo.Swarm = domain.SwarmInfo{
		NodeID:           info.Swarm.NodeID,
		NodeAddr:         info.Swarm.NodeAddr,
		LocalNodeState:   domain.LocalNodeState(info.Swarm.LocalNodeState),
		ControlAvailable: info.Swarm.ControlAvailable,
		Error:            info.Swarm.Error,
		Nodes:            info.Swarm.Nodes,
		Managers:         info.Swarm.Managers,
	}

	for _, rm := range info.Swarm.RemoteManagers {
		domainInfo.Swarm.RemoteManagers = append(domainInfo.Swarm.RemoteManagers, domain.Peer{
			NodeID: rm.NodeID,
			Addr:   rm.Addr,
		})
	}

	if info.Swarm.Cluster != nil {
		domainInfo.Swarm.Cluster = &domain.ClusterInfo{
			ID: info.Swarm.Cluster.ID,
			Version: domain.ServiceVersion{
				Index: info.Swarm.Cluster.Version.Index,
			},
			CreatedAt:              info.Swarm.Cluster.CreatedAt,
			UpdatedAt:              info.Swarm.Cluster.UpdatedAt,
			RootRotationInProgress: info.Swarm.Cluster.RootRotationInProgress,
		}
	}

	return domainInfo, nil
}

// Ping pings the Docker server.
func (s *SystemServiceAdapter) Ping(ctx context.Context) (*domain.PingResponse, error) {
	ping, err := s.client.Ping(ctx)
	if err != nil {
		return nil, convertError(err)
	}

	return &domain.PingResponse{
		APIVersion:     ping.APIVersion,
		OSType:         ping.OSType,
		Experimental:   ping.Experimental,
		BuilderVersion: string(ping.BuilderVersion),
	}, nil
}

// Version returns version information.
func (s *SystemServiceAdapter) Version(ctx context.Context) (*domain.Version, error) {
	version, err := s.client.ServerVersion(ctx)
	if err != nil {
		return nil, convertError(err)
	}

	domainVersion := &domain.Version{
		Platform: domain.Platform{
			Name: version.Platform.Name,
		},
		Version:       version.Version,
		APIVersion:    version.APIVersion,
		MinAPIVersion: version.MinAPIVersion,
		GitCommit:     version.GitCommit,
		GoVersion:     version.GoVersion,
		Os:            version.Os,
		Arch:          version.Arch,
		KernelVersion: version.KernelVersion,
		BuildTime:     version.BuildTime,
	}

	for _, comp := range version.Components {
		domainVersion.Components = append(domainVersion.Components, domain.ComponentVersion{
			Name:    comp.Name,
			Version: comp.Version,
			Details: comp.Details,
		})
	}

	return domainVersion, nil
}

// DiskUsage returns disk usage information.
func (s *SystemServiceAdapter) DiskUsage(ctx context.Context) (*domain.DiskUsage, error) {
	du, err := s.client.DiskUsage(ctx, types.DiskUsageOptions{})
	if err != nil {
		return nil, convertError(err)
	}

	domainDU := &domain.DiskUsage{
		LayersSize: du.LayersSize,
	}

	// Convert images
	for _, img := range du.Images {
		domainDU.Images = append(domainDU.Images, domain.ImageSummary{
			ID:          img.ID,
			ParentID:    img.ParentID,
			RepoTags:    img.RepoTags,
			RepoDigests: img.RepoDigests,
			Created:     img.Created,
			Size:        img.Size,
			SharedSize:  img.SharedSize,
			Labels:      img.Labels,
			Containers:  img.Containers,
		})
	}

	// Convert containers
	for _, c := range du.Containers {
		domainDU.Containers = append(domainDU.Containers, domain.ContainerSummary{
			ID:         c.ID,
			Names:      c.Names,
			Image:      c.Image,
			ImageID:    c.ImageID,
			Command:    c.Command,
			Created:    c.Created,
			State:      c.State,
			Status:     c.Status,
			SizeRw:     c.SizeRw,
			SizeRootFs: c.SizeRootFs,
		})
	}

	// Convert volumes
	for _, v := range du.Volumes {
		vol := domain.VolumeSummary{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			CreatedAt:  v.CreatedAt,
			Labels:     v.Labels,
			Scope:      v.Scope,
			Options:    v.Options,
		}
		if v.UsageData != nil {
			vol.UsageData = &domain.VolumeUsageData{
				Size:     v.UsageData.Size,
				RefCount: v.UsageData.RefCount,
			}
		}
		domainDU.Volumes = append(domainDU.Volumes, vol)
	}

	return domainDU, nil
}
