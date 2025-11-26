package docker

import (
	"context"
	"time"

	"github.com/docker/docker/api/types"
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

// Create creates a new Swarm service.
func (s *SwarmServiceAdapter) Create(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error) {
	swarmSpec := convertToSwarmSpec(&spec)

	createOpts := types.ServiceCreateOptions{
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
	service, _, err := s.client.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return nil, convertError(err)
	}

	return convertFromSwarmService(&service), nil
}

// List lists services.
func (s *SwarmServiceAdapter) List(ctx context.Context, opts domain.ServiceListOptions) ([]domain.Service, error) {
	listOpts := types.ServiceListOptions{}

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
	err := s.client.ServiceRemove(ctx, serviceID)
	return convertError(err)
}

// ListTasks lists tasks.
func (s *SwarmServiceAdapter) ListTasks(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error) {
	listOpts := types.TaskListOptions{}

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

			// Check if all tasks are in terminal state
			allTerminal := true
			for _, task := range tasks {
				if !task.Status.State.IsTerminalState() {
					allTerminal = false
					break
				}
			}
			if allTerminal {
				return tasks, nil
			}
		}
	}
}

// Conversion functions

func convertToSwarmSpec(spec *domain.ServiceSpec) swarm.ServiceSpec {
	swarmSpec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   spec.Name,
			Labels: spec.Labels,
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:     spec.TaskTemplate.ContainerSpec.Image,
				Labels:    spec.TaskTemplate.ContainerSpec.Labels,
				Command:   spec.TaskTemplate.ContainerSpec.Command,
				Args:      spec.TaskTemplate.ContainerSpec.Args,
				Hostname:  spec.TaskTemplate.ContainerSpec.Hostname,
				Env:       spec.TaskTemplate.ContainerSpec.Env,
				Dir:       spec.TaskTemplate.ContainerSpec.Dir,
				User:      spec.TaskTemplate.ContainerSpec.User,
				TTY:       spec.TaskTemplate.ContainerSpec.TTY,
				OpenStdin: spec.TaskTemplate.ContainerSpec.OpenStdin,
			},
		},
	}

	// Convert mounts
	for _, m := range spec.TaskTemplate.ContainerSpec.Mounts {
		swarmSpec.TaskTemplate.ContainerSpec.Mounts = append(
			swarmSpec.TaskTemplate.ContainerSpec.Mounts,
			mount.Mount{
				Type:     mount.Type(m.Type),
				Source:   m.Source,
				Target:   m.Target,
				ReadOnly: m.ReadOnly,
			},
		)
	}

	// Convert restart policy
	if spec.TaskTemplate.RestartPolicy != nil {
		swarmSpec.TaskTemplate.RestartPolicy = &swarm.RestartPolicy{
			Condition:   swarm.RestartPolicyCondition(spec.TaskTemplate.RestartPolicy.Condition),
			Delay:       spec.TaskTemplate.RestartPolicy.Delay,
			MaxAttempts: spec.TaskTemplate.RestartPolicy.MaxAttempts,
			Window:      spec.TaskTemplate.RestartPolicy.Window,
		}
	}

	// Convert resources
	if spec.TaskTemplate.Resources != nil {
		swarmSpec.TaskTemplate.Resources = &swarm.ResourceRequirements{}
		if spec.TaskTemplate.Resources.Limits != nil {
			swarmSpec.TaskTemplate.Resources.Limits = &swarm.Limit{
				NanoCPUs:    spec.TaskTemplate.Resources.Limits.NanoCPUs,
				MemoryBytes: spec.TaskTemplate.Resources.Limits.MemoryBytes,
			}
		}
		if spec.TaskTemplate.Resources.Reservations != nil {
			swarmSpec.TaskTemplate.Resources.Reservations = &swarm.Resources{
				NanoCPUs:    spec.TaskTemplate.Resources.Reservations.NanoCPUs,
				MemoryBytes: spec.TaskTemplate.Resources.Reservations.MemoryBytes,
			}
		}
	}

	// Convert networks
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

	return swarmSpec
}

func convertFromSwarmService(svc *swarm.Service) *domain.Service {
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

	// Convert task template
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		service.Spec.TaskTemplate.ContainerSpec = domain.ContainerSpec{
			Image:     svc.Spec.TaskTemplate.ContainerSpec.Image,
			Labels:    svc.Spec.TaskTemplate.ContainerSpec.Labels,
			Command:   svc.Spec.TaskTemplate.ContainerSpec.Command,
			Args:      svc.Spec.TaskTemplate.ContainerSpec.Args,
			Hostname:  svc.Spec.TaskTemplate.ContainerSpec.Hostname,
			Env:       svc.Spec.TaskTemplate.ContainerSpec.Env,
			Dir:       svc.Spec.TaskTemplate.ContainerSpec.Dir,
			User:      svc.Spec.TaskTemplate.ContainerSpec.User,
			TTY:       svc.Spec.TaskTemplate.ContainerSpec.TTY,
			OpenStdin: svc.Spec.TaskTemplate.ContainerSpec.OpenStdin,
		}
	}

	return service
}

func convertFromSwarmTask(task *swarm.Task) domain.Task {
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
