// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"time"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"

	"github.com/netresearch/ofelia/core/domain"
)

// SwarmServiceAdapter implements ports.SwarmService using Docker SDK.
type SwarmServiceAdapter struct {
	client *client.Client
}

// checkClient returns ErrNilDockerClient if the embedded SDK client is nil.
// See docker.ErrNilDockerClient for rationale.
func (s *SwarmServiceAdapter) checkClient() error {
	if s.client == nil {
		return ErrNilDockerClient
	}
	return nil
}

// Create creates a new Swarm service.
func (s *SwarmServiceAdapter) Create(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error) {
	if err := s.checkClient(); err != nil {
		return "", err
	}
	swarmSpec := convertToSwarmSpec(&spec)

	createOpts := swarm.ServiceCreateOptions{
		EncodedRegistryAuth: opts.EncodedRegistryAuth,
	}

	resp, err := s.client.ServiceCreate(ctx, swarmSpec, createOpts)
	if err != nil {
		return "", convertError(err)
	}

	return resp.ID, nil
}

// Inspect returns service information.
func (s *SwarmServiceAdapter) Inspect(ctx context.Context, serviceID string) (*domain.Service, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	service, _, err := s.client.ServiceInspectWithRaw(ctx, serviceID, swarm.ServiceInspectOptions{})
	if err != nil {
		return nil, convertError(err)
	}

	return convertFromSwarmService(&service), nil
}

// List lists services.
func (s *SwarmServiceAdapter) List(ctx context.Context, opts domain.ServiceListOptions) ([]domain.Service, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	listOpts := swarm.ServiceListOptions{}

	if len(opts.Filters) > 0 {
		listOpts.Filters = filters.NewArgs()
		for key, values := range opts.Filters {
			for _, v := range values {
				listOpts.Filters.Add(key, v)
			}
		}
	}

	services, err := s.client.ServiceList(ctx, listOpts)
	if err != nil {
		return nil, convertError(err)
	}

	result := make([]domain.Service, len(services))
	for i, svc := range services {
		result[i] = *convertFromSwarmService(&svc)
	}
	return result, nil
}

// Remove removes a service.
func (s *SwarmServiceAdapter) Remove(ctx context.Context, serviceID string) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	err := s.client.ServiceRemove(ctx, serviceID)
	return convertError(err)
}

// ListTasks lists tasks.
func (s *SwarmServiceAdapter) ListTasks(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	listOpts := swarm.TaskListOptions{}

	if len(opts.Filters) > 0 {
		listOpts.Filters = filters.NewArgs()
		for key, values := range opts.Filters {
			for _, v := range values {
				listOpts.Filters.Add(key, v)
			}
		}
	}

	tasks, err := s.client.TaskList(ctx, listOpts)
	if err != nil {
		return nil, convertError(err)
	}

	result := make([]domain.Task, len(tasks))
	for i, task := range tasks {
		result[i] = convertFromSwarmTask(&task)
	}
	return result, nil
}

// WaitForTask waits for a task to reach a terminal state.
func (s *SwarmServiceAdapter) WaitForTask(ctx context.Context, taskID string, timeout time.Duration) (*domain.Task, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, domain.ErrTimeout
		case <-ticker.C:
			tasks, err := s.ListTasks(ctx, domain.TaskListOptions{
				Filters: map[string][]string{
					"id": {taskID},
				},
			})
			if err != nil {
				return nil, err
			}
			if len(tasks) == 0 {
				continue
			}
			task := &tasks[0]
			if task.Status.State.IsTerminalState() {
				return task, nil
			}
		}
	}
}

// WaitForServiceTasks waits for all service tasks to reach a terminal state.
func (s *SwarmServiceAdapter) WaitForServiceTasks(ctx context.Context, serviceID string, timeout time.Duration) ([]domain.Task, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, domain.ErrTimeout
		case <-ticker.C:
			tasks, err := s.ListTasks(ctx, domain.TaskListOptions{
				Filters: map[string][]string{
					"service": {serviceID},
				},
			})
			if err != nil {
				return nil, err
			}
			if len(tasks) == 0 {
				continue
			}

			if allTasksTerminal(tasks) {
				return tasks, nil
			}
		}
	}
}

// allTasksTerminal returns true when every task in the slice has reached a
// terminal state. Returns true for an empty slice (vacuously true).
func allTasksTerminal(tasks []domain.Task) bool {
	for _, task := range tasks {
		if !task.Status.State.IsTerminalState() {
			return false
		}
	}
	return true
}

// Conversion functions

// convertToSwarmSpec converts a domain ServiceSpec to the SDK's
// swarm.ServiceSpec. Returns the zero value swarm.ServiceSpec{} when spec is
// nil; the Docker SDK rejects the empty spec server-side, so callers get a
// clean error rather than a panic.
func convertToSwarmSpec(spec *domain.ServiceSpec) swarm.ServiceSpec {
	if spec == nil {
		return swarm.ServiceSpec{}
	}

	swarmSpec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   spec.Name,
			Labels: spec.Labels,
		},
	}

	convertTaskTemplateToSwarm(&spec.TaskTemplate, &swarmSpec.TaskTemplate)

	// Convert networks from both ServiceSpec and TaskTemplate levels.
	// buildService() writes to TaskTemplate.Networks; both locations are valid.
	for _, n := range spec.Networks {
		swarmSpec.TaskTemplate.Networks = append(swarmSpec.TaskTemplate.Networks, swarm.NetworkAttachmentConfig{
			Target:  n.Target,
			Aliases: n.Aliases,
		})
	}

	// Convert mode
	if spec.Mode.Replicated != nil {
		swarmSpec.Mode = swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{
				Replicas: spec.Mode.Replicated.Replicas,
			},
		}
	} else if spec.Mode.Global != nil {
		swarmSpec.Mode = swarm.ServiceMode{
			Global: &swarm.GlobalService{},
		}
	}

	// Convert endpoint spec
	if spec.EndpointSpec != nil {
		swarmSpec.EndpointSpec = &swarm.EndpointSpec{
			Mode: swarm.ResolutionMode(spec.EndpointSpec.Mode),
		}
		for _, p := range spec.EndpointSpec.Ports {
			swarmSpec.EndpointSpec.Ports = append(swarmSpec.EndpointSpec.Ports, swarm.PortConfig{
				Name:          p.Name,
				Protocol:      swarm.PortConfigProtocol(p.Protocol),
				TargetPort:    p.TargetPort,
				PublishedPort: p.PublishedPort,
				PublishMode:   swarm.PortConfigPublishMode(p.PublishMode),
			})
		}
	}

	return swarmSpec
}

// convertTaskTemplateToSwarm fills the SDK swarm.TaskSpec from the domain
// TaskSpec. A nil src or nil dst short-circuits to a no-op so production
// callers (which always pass non-nil pointers) keep their existing behavior
// while test callers can pass nil without panicking.
func convertTaskTemplateToSwarm(src *domain.TaskSpec, dst *swarm.TaskSpec) {
	if src == nil || dst == nil {
		return
	}

	dst.ContainerSpec = &swarm.ContainerSpec{
		Image:     src.ContainerSpec.Image,
		Labels:    src.ContainerSpec.Labels,
		Command:   src.ContainerSpec.Command,
		Args:      src.ContainerSpec.Args,
		Hostname:  src.ContainerSpec.Hostname,
		Env:       src.ContainerSpec.Env,
		Dir:       src.ContainerSpec.Dir,
		User:      src.ContainerSpec.User,
		TTY:       src.ContainerSpec.TTY,
		OpenStdin: src.ContainerSpec.OpenStdin,
	}

	for _, m := range src.ContainerSpec.Mounts {
		dst.ContainerSpec.Mounts = append(dst.ContainerSpec.Mounts, mount.Mount{
			Type:     mount.Type(m.Type),
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}

	if src.RestartPolicy != nil {
		dst.RestartPolicy = &swarm.RestartPolicy{
			Condition:   swarm.RestartPolicyCondition(src.RestartPolicy.Condition),
			Delay:       src.RestartPolicy.Delay,
			MaxAttempts: src.RestartPolicy.MaxAttempts,
			Window:      src.RestartPolicy.Window,
		}
	}

	if src.Resources != nil {
		dst.Resources = &swarm.ResourceRequirements{}
		if src.Resources.Limits != nil {
			dst.Resources.Limits = &swarm.Limit{
				NanoCPUs:    src.Resources.Limits.NanoCPUs,
				MemoryBytes: src.Resources.Limits.MemoryBytes,
			}
		}
		if src.Resources.Reservations != nil {
			dst.Resources.Reservations = &swarm.Resources{
				NanoCPUs:    src.Resources.Reservations.NanoCPUs,
				MemoryBytes: src.Resources.Reservations.MemoryBytes,
			}
		}
	}

	if src.Placement != nil {
		dst.Placement = convertPlacementToSwarm(src.Placement)
	}

	if src.LogDriver != nil {
		dst.LogDriver = &swarm.Driver{
			Name:    src.LogDriver.Name,
			Options: src.LogDriver.Options,
		}
	}

	for _, n := range src.Networks {
		dst.Networks = append(dst.Networks, swarm.NetworkAttachmentConfig{
			Target:  n.Target,
			Aliases: n.Aliases,
		})
	}
}

// convertPlacementToSwarm converts a domain Placement to the SDK swarm.Placement.
func convertPlacementToSwarm(src *domain.Placement) *swarm.Placement {
	dst := &swarm.Placement{
		Constraints: src.Constraints,
	}
	for _, pref := range src.Preferences {
		sp := swarm.PlacementPreference{}
		if pref.Spread != nil {
			sp.Spread = &swarm.SpreadOver{
				SpreadDescriptor: pref.Spread.SpreadDescriptor,
			}
		}
		dst.Preferences = append(dst.Preferences, sp)
	}
	return dst
}

// convertFromSwarmService translates a Docker SDK *swarm.Service to its
// domain counterpart. Returns nil when svc is nil — the inverse contract
// of convertToSwarmSpec(nil) -> swarm.ServiceSpec{}, see #632 / #626.
func convertFromSwarmService(svc *swarm.Service) *domain.Service {
	if svc == nil {
		return nil
	}

	service := &domain.Service{
		ID: svc.ID,
		Meta: domain.ServiceMeta{
			Version: domain.ServiceVersion{
				Index: svc.Version.Index,
			},
			CreatedAt: svc.CreatedAt,
			UpdatedAt: svc.UpdatedAt,
		},
		Spec: domain.ServiceSpec{
			Name:   svc.Spec.Name,
			Labels: svc.Spec.Labels,
		},
	}

	convertTaskTemplateFromSwarm(&svc.Spec.TaskTemplate, &service.Spec.TaskTemplate)

	// Convert mode
	if svc.Spec.Mode.Replicated != nil {
		service.Spec.Mode.Replicated = &domain.ReplicatedService{
			Replicas: svc.Spec.Mode.Replicated.Replicas,
		}
	} else if svc.Spec.Mode.Global != nil {
		service.Spec.Mode.Global = &domain.GlobalService{}
	}

	// Convert endpoint spec
	if svc.Spec.EndpointSpec != nil {
		service.Spec.EndpointSpec = &domain.EndpointSpec{
			Mode: domain.ResolutionMode(svc.Spec.EndpointSpec.Mode),
		}
		for _, p := range svc.Spec.EndpointSpec.Ports {
			service.Spec.EndpointSpec.Ports = append(service.Spec.EndpointSpec.Ports, domain.PortConfig{
				Name:          p.Name,
				Protocol:      domain.PortProtocol(p.Protocol),
				TargetPort:    p.TargetPort,
				PublishedPort: p.PublishedPort,
				PublishMode:   domain.PortPublishMode(p.PublishMode),
			})
		}
	}

	return service
}

// convertTaskTemplateFromSwarm copies fields from a Docker SDK
// *swarm.TaskSpec into a domain *TaskSpec. No-op (returns silently)
// when either src or dst is nil — symmetric with the
// convertTaskTemplateToSwarm guard from #626. See #632.
func convertTaskTemplateFromSwarm(src *swarm.TaskSpec, dst *domain.TaskSpec) {
	if src == nil || dst == nil {
		return
	}

	if src.ContainerSpec != nil {
		cs := src.ContainerSpec
		dst.ContainerSpec = domain.ContainerSpec{
			Image:     cs.Image,
			Labels:    cs.Labels,
			Command:   cs.Command,
			Args:      cs.Args,
			Hostname:  cs.Hostname,
			Env:       cs.Env,
			Dir:       cs.Dir,
			User:      cs.User,
			TTY:       cs.TTY,
			OpenStdin: cs.OpenStdin,
		}
		for _, m := range cs.Mounts {
			dst.ContainerSpec.Mounts = append(dst.ContainerSpec.Mounts, domain.ServiceMount{
				Type:     domain.MountType(m.Type),
				Source:   m.Source,
				Target:   m.Target,
				ReadOnly: m.ReadOnly,
			})
		}
	}

	if src.RestartPolicy != nil {
		rp := src.RestartPolicy
		dst.RestartPolicy = &domain.ServiceRestartPolicy{
			Condition:   domain.RestartCondition(rp.Condition),
			Delay:       rp.Delay,
			MaxAttempts: rp.MaxAttempts,
			Window:      rp.Window,
		}
	}

	if src.Resources != nil {
		dst.Resources = &domain.ResourceRequirements{}
		if src.Resources.Limits != nil {
			dst.Resources.Limits = &domain.Resources{
				NanoCPUs:    src.Resources.Limits.NanoCPUs,
				MemoryBytes: src.Resources.Limits.MemoryBytes,
			}
		}
		if src.Resources.Reservations != nil {
			dst.Resources.Reservations = &domain.Resources{
				NanoCPUs:    src.Resources.Reservations.NanoCPUs,
				MemoryBytes: src.Resources.Reservations.MemoryBytes,
			}
		}
	}

	for _, n := range src.Networks {
		dst.Networks = append(dst.Networks, domain.NetworkAttachment{
			Target:  n.Target,
			Aliases: n.Aliases,
		})
	}

	if src.Placement != nil {
		dst.Placement = convertPlacementFromSwarm(src.Placement)
	}

	if src.LogDriver != nil {
		dst.LogDriver = &domain.LogDriver{
			Name:    src.LogDriver.Name,
			Options: src.LogDriver.Options,
		}
	}
}

// convertPlacementFromSwarm converts an SDK swarm.Placement to the domain Placement.
func convertPlacementFromSwarm(src *swarm.Placement) *domain.Placement {
	dst := &domain.Placement{
		Constraints: src.Constraints,
	}
	for _, pref := range src.Preferences {
		dp := domain.PlacementPreference{}
		if pref.Spread != nil {
			dp.Spread = &domain.SpreadOver{
				SpreadDescriptor: pref.Spread.SpreadDescriptor,
			}
		}
		dst.Preferences = append(dst.Preferences, dp)
	}
	return dst
}

// convertFromSwarmTask translates a Docker SDK *swarm.Task to its domain
// counterpart. Returns the zero domain.Task when task is nil — guards
// against an SDK list ever yielding a nil entry. See #632.
func convertFromSwarmTask(task *swarm.Task) domain.Task {
	if task == nil {
		return domain.Task{}
	}

	domainTask := domain.Task{
		ID:           task.ID,
		ServiceID:    task.ServiceID,
		NodeID:       task.NodeID,
		DesiredState: domain.TaskState(task.DesiredState),
		CreatedAt:    task.CreatedAt,
		UpdatedAt:    task.UpdatedAt,
		Status: domain.TaskStatus{
			Timestamp: task.Status.Timestamp,
			State:     domain.TaskState(task.Status.State),
			Message:   task.Status.Message,
			Err:       task.Status.Err,
		},
	}

	if task.Status.ContainerStatus != nil {
		domainTask.Status.ContainerStatus = &domain.ContainerStatus{
			ContainerID: task.Status.ContainerStatus.ContainerID,
			PID:         task.Status.ContainerStatus.PID,
			ExitCode:    task.Status.ContainerStatus.ExitCode,
		}
	}

	return domainTask
}
