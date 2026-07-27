// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"

	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"

	"github.com/netresearch/ofelia/core/domain"
)

// SystemServiceAdapter implements ports.SystemService using Docker SDK.
type SystemServiceAdapter struct {
	client *client.Client
}

// checkClient returns ErrNilDockerClient if the embedded SDK client is nil.
// See docker.ErrNilDockerClient for rationale.
func (s *SystemServiceAdapter) checkClient() error {
	if s.client == nil {
		return ErrNilDockerClient
	}
	return nil
}

// Info returns system information.
func (s *SystemServiceAdapter) Info(ctx context.Context) (*domain.SystemInfo, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	infoResult, err := s.client.Info(ctx, client.InfoOptions{})
	if err != nil {
		return nil, convertError(err)
	}

	domainInfo := &domain.SystemInfo{
		ID:                 infoResult.Info.ID,
		Containers:         infoResult.Info.Containers,
		ContainersRunning:  infoResult.Info.ContainersRunning,
		ContainersPaused:   infoResult.Info.ContainersPaused,
		ContainersStopped:  infoResult.Info.ContainersStopped,
		Images:             infoResult.Info.Images,
		Driver:             infoResult.Info.Driver,
		MemoryLimit:        infoResult.Info.MemoryLimit,
		SwapLimit:          infoResult.Info.SwapLimit,
		CPUCfsPeriod:       infoResult.Info.CPUCfsPeriod,
		CPUCfsQuota:        infoResult.Info.CPUCfsQuota,
		CPUShares:          infoResult.Info.CPUShares,
		CPUSet:             infoResult.Info.CPUSet,
		PidsLimit:          infoResult.Info.PidsLimit,
		IPv4Forwarding:     infoResult.Info.IPv4Forwarding,
		Debug:              infoResult.Info.Debug,
		NFd:                infoResult.Info.NFd,
		OomKillDisable:     infoResult.Info.OomKillDisable,
		NGoroutines:        infoResult.Info.NGoroutines,
		SystemTime:         infoResult.Info.SystemTime,
		LoggingDriver:      infoResult.Info.LoggingDriver,
		CgroupDriver:       infoResult.Info.CgroupDriver,
		CgroupVersion:      infoResult.Info.CgroupVersion,
		NEventsListener:    infoResult.Info.NEventsListener,
		KernelVersion:      infoResult.Info.KernelVersion,
		OperatingSystem:    infoResult.Info.OperatingSystem,
		OSVersion:          infoResult.Info.OSVersion,
		OSType:             infoResult.Info.OSType,
		Architecture:       infoResult.Info.Architecture,
		IndexServerAddress: infoResult.Info.IndexServerAddress,
		NCPU:               infoResult.Info.NCPU,
		MemTotal:           infoResult.Info.MemTotal,
		DockerRootDir:      infoResult.Info.DockerRootDir,
		HTTPProxy:          infoResult.Info.HTTPProxy,
		HTTPSProxy:         infoResult.Info.HTTPSProxy,
		NoProxy:            infoResult.Info.NoProxy,
		Name:               infoResult.Info.Name,
		Labels:             infoResult.Info.Labels,
		ExperimentalBuild:  infoResult.Info.ExperimentalBuild,
		ServerVersion:      infoResult.Info.ServerVersion,
		DefaultRuntime:     infoResult.Info.DefaultRuntime,
		LiveRestoreEnabled: infoResult.Info.LiveRestoreEnabled,
		Isolation:          string(infoResult.Info.Isolation),
		InitBinary:         infoResult.Info.InitBinary,
		SecurityOptions:    infoResult.Info.SecurityOptions,
		Warnings:           infoResult.Info.Warnings,
	}

	// Convert driver status
	for _, ds := range infoResult.Info.DriverStatus {
		domainInfo.DriverStatus = append(domainInfo.DriverStatus, [2]string{ds[0], ds[1]})
	}

	// Convert system status
	for _, ss := range infoResult.Info.SystemStatus {
		domainInfo.SystemStatus = append(domainInfo.SystemStatus, [2]string{ss[0], ss[1]})
	}

	// Convert runtimes
	if len(infoResult.Info.Runtimes) > 0 {
		domainInfo.Runtimes = make(map[string]domain.Runtime)
		for name, rt := range infoResult.Info.Runtimes {
			domainInfo.Runtimes[name] = domain.Runtime{
				Path: rt.Path,
				Args: rt.Args,
			}
		}
	}

	// Convert swarm info
	domainInfo.Swarm = domain.SwarmInfo{
		NodeID:           infoResult.Info.Swarm.NodeID,
		NodeAddr:         infoResult.Info.Swarm.NodeAddr,
		LocalNodeState:   domain.LocalNodeState(infoResult.Info.Swarm.LocalNodeState),
		ControlAvailable: infoResult.Info.Swarm.ControlAvailable,
		Error:            infoResult.Info.Swarm.Error,
		Nodes:            infoResult.Info.Swarm.Nodes,
		Managers:         infoResult.Info.Swarm.Managers,
	}

	for _, rm := range infoResult.Info.Swarm.RemoteManagers {
		domainInfo.Swarm.RemoteManagers = append(domainInfo.Swarm.RemoteManagers, domain.Peer{
			NodeID: rm.NodeID,
			Addr:   rm.Addr,
		})
	}

	if infoResult.Info.Swarm.Cluster != nil {
		domainInfo.Swarm.Cluster = &domain.ClusterInfo{
			ID: infoResult.Info.Swarm.Cluster.ID,
			Version: domain.ServiceVersion{
				Index: infoResult.Info.Swarm.Cluster.Version.Index,
			},
			CreatedAt:              infoResult.Info.Swarm.Cluster.CreatedAt,
			UpdatedAt:              infoResult.Info.Swarm.Cluster.UpdatedAt,
			RootRotationInProgress: infoResult.Info.Swarm.Cluster.RootRotationInProgress,
		}
	}

	return domainInfo, nil
}

// engineDetail reads a key from the "Engine" component's Details map of the
// /version response. ServerVersionResult no longer carries GitCommit,
// GoVersion, KernelVersion and BuildTime as typed fields; the daemon still
// reports them here. Upstream documents Details as informational and outside
// the API specification, so a missing key yields "" rather than an error.
func engineDetail(components []system.ComponentVersion, key string) string {
	for _, comp := range components {
		if comp.Name != "Engine" {
			continue
		}
		return comp.Details[key]
	}
	return ""
}

// Ping pings the Docker server.
func (s *SystemServiceAdapter) Ping(ctx context.Context) (*domain.PingResponse, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	ping, err := s.client.Ping(ctx, client.PingOptions{})
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
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	version, err := s.client.ServerVersion(ctx, client.ServerVersionOptions{})
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
		GitCommit:     engineDetail(version.Components, "GitCommit"),
		GoVersion:     engineDetail(version.Components, "GoVersion"),
		Os:            version.Os,
		Arch:          version.Arch,
		KernelVersion: engineDetail(version.Components, "KernelVersion"),
		BuildTime:     engineDetail(version.Components, "BuildTime"),
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
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	du, err := s.client.DiskUsage(ctx, client.DiskUsageOptions{})
	if err != nil {
		return nil, convertError(err)
	}

	domainDU := &domain.DiskUsage{
		LayersSize: du.Images.TotalSize,
	}

	// Convert images
	for _, img := range du.Images.Items {
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
	for _, c := range du.Containers.Items {
		domainDU.Containers = append(domainDU.Containers, domain.ContainerSummary{
			ID:         c.ID,
			Names:      c.Names,
			Image:      c.Image,
			ImageID:    c.ImageID,
			Command:    c.Command,
			Created:    c.Created,
			State:      string(c.State),
			Status:     c.Status,
			SizeRw:     c.SizeRw,
			SizeRootFs: c.SizeRootFs,
		})
	}

	// Convert volumes
	for _, v := range du.Volumes.Items {
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
