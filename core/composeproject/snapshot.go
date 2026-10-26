// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package composeproject

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/netresearch/ofelia/core/domain"
)

const secretResourcePrefix = "secrets/"

const snapshotResourceLabel = "ofelia.compose.resources"

// snapshotProject resolves inputs once, before either revisions or the
// execution model are accepted. Inactive definitions stay in the snapshot:
// an explicitly run job can activate a dependency behind another profile.
func snapshotProject(registration string, source *types.Project) (*Project, error) {
	accepted, err := Extract(registration, source)
	if err != nil {
		return nil, err
	}
	resolved, err := resolveExecutionEnvironment(selectExecutionProject(source, accepted.Jobs))
	if err != nil {
		return nil, fmt.Errorf("compose project %q: %w", registration, err)
	}
	files, environment, err := captureFileObjects(resolved)
	if err != nil {
		return nil, fmt.Errorf("compose project %q: %w", registration, err)
	}
	if err := labelServiceResources(resolved, files, environment); err != nil {
		return nil, fmt.Errorf("compose project %q: %w", registration, err)
	}
	model, err := resolved.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("compose project %q: marshal execution snapshot: %w", registration, err)
	}
	// Apply the original profile selection for extraction only; the execution
	// snapshot above retains definitions that Compose can activate on demand.
	active, err := resolved.WithProfiles(source.Profiles)
	if err != nil {
		return nil, fmt.Errorf("compose project %q: apply profiles: %w", registration, err)
	}
	out, err := Extract(registration, active)
	if err != nil {
		return nil, err
	}
	out.Snapshot, out.SnapshotFiles = string(model), files
	out.SnapshotEnvironment = environment
	out.Unscheduled = accepted.Unscheduled
	out.Revision, err = snapshotRevision(json.RawMessage(model), files, environment, source.Profiles)
	if err != nil {
		return nil, err
	}
	for i := range out.Jobs {
		job := &out.Jobs[i]
		closure := map[string]any{}
		collectClosure(job.Name, resolved.Jobs, resolved.Services, closure)
		resources, inputs := executionResources(closure, resolved, files)
		job.Revision, err = snapshotRevision(closure, resources, inputs, resourceEnvironment(resources, environment))
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Compose hashes a service's references, not the referenced config/secret
// contents. Make accepted input changes visible in that hash, so an existing
// dependency cannot keep mounting an older retained input after a valid reload.
// The fingerprint excludes jobs, scheduling, profile selection and temp paths.
func labelServiceResources(project *types.Project, files map[string]domain.ComposeSnapshotFile, environment map[string]string) error {
	for name, service := range project.Services {
		allResources, inputs := executionResources(map[string]any{"service:" + name: service}, project, files)
		resources := map[string]any{}
		for key, resource := range allResources {
			switch resource.(type) {
			case types.ConfigObjConfig, types.SecretConfig:
				resources[key] = resource
			}
		}
		if len(resources) == 0 {
			continue
		}
		revision, err := snapshotRevision(resources, inputs, resourceEnvironment(resources, environment))
		if err != nil {
			return err
		}
		service.Labels = maps.Clone(service.Labels)
		if service.Labels == nil {
			service.Labels = types.Labels{}
		}
		service.Labels[snapshotResourceLabel] = revision
		project.Services[name] = service
	}
	return nil
}

// Select only scheduled jobs and their dependencies before reading file
// inputs. An unrelated inactive profile must not need its env/config files
// installed merely to arm an active job. Dependencies behind profiles remain
// in the execution model so Compose can activate them on demand.
func selectExecutionProject(source *types.Project, scheduled []Job) *types.Project {
	jobs, services := source.AllJobs(), source.AllServices()
	for name, job := range jobs {
		job.DependsOn = activeExecutionDependencies(job.DependsOn, source)
		jobs[name] = job
	}
	for name, service := range services {
		service.DependsOn = activeExecutionDependencies(service.DependsOn, source)
		services[name] = service
	}
	closure := map[string]any{}
	for _, job := range scheduled {
		collectClosure(job.Name, jobs, services, closure)
	}
	project := *source
	project.Services, project.Jobs = types.Services{}, types.Jobs{}
	project.DisabledServices, project.DisabledJobs = nil, nil
	for name, definition := range closure {
		switch def := definition.(type) {
		case types.JobConfig:
			project.Jobs[name[len("job:"):]] = def
		case types.ServiceConfig:
			project.Services[name[len("service:"):]] = def
		}
	}
	resources, _ := executionResources(closure, &project, nil)
	project.Configs = selectedResources("configs", source.Configs, resources)
	project.Secrets = selectedResources("secrets", source.Secrets, resources)
	project.Networks = selectedResources("networks", source.Networks, resources)
	project.Volumes = selectedResources("volumes", source.Volumes, resources)
	project.Models = selectedResources("models", source.Models, resources)
	return &project
}

// Optional dependencies behind inactive profiles do not run and must not need
// their environment or config files. Keep required edges: Compose itself
// validates or activates those, and their accepted inputs belong to the model.
func activeExecutionDependencies(dependencies types.DependsOnConfig, source *types.Project) types.DependsOnConfig {
	out := types.DependsOnConfig{}
	for name, dependency := range dependencies {
		_, jobActive := source.Jobs[name]
		_, serviceActive := source.Services[name]
		if dependency.Required || jobActive || serviceActive {
			out[name] = dependency
		}
	}
	return out
}

// selectedResources copies only referenced top-level definitions, keeping unrelated inactive configuration
// inputs outside the accepted execution closure.
func selectedResources[T any](kind string, all map[string]T, used map[string]any) map[string]T {
	out := map[string]T{}
	for name, value := range all {
		if _, ok := used[kind+"/"+name]; ok {
			out[name] = value
		}
	}
	return out
}

// compose-go resolves service environments, but its job container specs need
// the same resolution. Reuse that implementation for jobs and their deps,
// including env_file format/precedence and label_file parsing.
func resolveExecutionEnvironment(project *types.Project) (*types.Project, error) {
	jobs := &types.Project{Environment: project.Environment, Services: types.Services{}}
	for name, job := range project.Jobs {
		jobs.Services[name] = types.ServiceConfig{ContainerSpec: job.ContainerSpec}
	}
	jobs, err := jobs.WithServicesEnvironmentResolved(true)
	if err != nil {
		return nil, fmt.Errorf("resolve job environment: %w", err)
	}
	jobs, err = jobs.WithServicesLabelsResolved(true)
	if err != nil {
		return nil, fmt.Errorf("resolve job labels: %w", err)
	}
	project, err = project.WithServicesEnvironmentResolved(true)
	if err != nil {
		return nil, fmt.Errorf("resolve service environment: %w", err)
	}
	project, err = project.WithServicesLabelsResolved(true)
	if err != nil {
		return nil, fmt.Errorf("resolve service labels: %w", err)
	}
	for name, job := range project.Jobs {
		job.ContainerSpec = jobs.Services[name].ContainerSpec
		project.Jobs[name] = job
	}
	return project, nil
}

// captureFileObjects freezes selected file and environment inputs while retaining each resource's native
// Compose bind-mount or upload semantics.
func captureFileObjects(project *types.Project) (map[string]domain.ComposeSnapshotFile, map[string]string, error) {
	files, environment := map[string]domain.ComposeSnapshotFile{}, map[string]string{}
	for name, cfg := range project.Configs {
		captured, err := captureFileObject(types.FileObjectConfig(cfg), "configs/"+name, project.Environment, files, environment)
		if err != nil {
			return nil, nil, err
		}
		project.Configs[name] = types.ConfigObjConfig(captured)
	}
	for name, secret := range project.Secrets {
		captured, err := captureFileObject(
			types.FileObjectConfig(secret), secretResourcePrefix+name, project.Environment, files, environment,
		)
		if err != nil {
			return nil, nil, err
		}
		project.Secrets[name] = types.SecretConfig(captured)
	}
	return files, environment, nil
}

// captureFileObject captures one resource without consulting operator inputs again at execution time;
// environment-backed resources receive isolated synthetic names.
func captureFileObject(cfg types.FileObjectConfig, key string, env types.Mapping,
	files map[string]domain.ComposeSnapshotFile, environment map[string]string,
) (types.FileObjectConfig, error) {
	if cfg.External || cfg.File == "" && cfg.Environment == "" {
		return cfg, nil
	}
	if cfg.File != "" {
		input, err := captureSnapshotFile(cfg.File)
		if err != nil {
			return cfg, fmt.Errorf("snapshot %s: %w", key, err)
		}
		files[key] = input
		cfg.File = "ofelia-snapshot/" + key
	} else {
		value, ok := env[cfg.Environment]
		if !ok {
			return cfg, fmt.Errorf("%w: snapshot %s: environment variable %q is not set", ErrInvalidSpec, key, cfg.Environment)
		}
		cfg.Environment = "OFELIA_SNAPSHOT_" + strings.ToUpper(shortRevision([]byte(key)))
		environment[cfg.Environment] = value
	}
	cfg.Content = ""
	return cfg, nil
}

// captureSnapshotFile reads bytes and access metadata from the same opened regular inode, avoiding path
// replacement between content capture and identity inspection.
func captureSnapshotFile(path string) (domain.ComposeSnapshotFile, error) {
	file, err := os.Open(path) // #nosec G304 -- registered Compose config/secret input, read at acceptance
	if err != nil {
		return domain.ComposeSnapshotFile{}, fmt.Errorf("open configuration input: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return domain.ComposeSnapshotFile{}, fmt.Errorf("inspect configuration input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return domain.ComposeSnapshotFile{}, fmt.Errorf("%w: configuration input must be a regular file", ErrInvalidSpec)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return domain.ComposeSnapshotFile{}, fmt.Errorf("read configuration input: %w", err)
	}
	uid, gid := snapshotFileIdentity(info)
	return domain.ComposeSnapshotFile{Content: content, Mode: uint32(info.Mode().Perm()), UID: uid, GID: gid}, nil
}

// snapshotRevision hashes accepted input values together and propagates serialization errors instead of
// claiming a revision that omitted data.
func snapshotRevision(values ...any) (string, error) {
	data, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("marshal execution revision: %w", err)
	}
	return shortRevision(data), nil
}

// executionResources adds only resources used by the dependency closure, so
// changing an unrelated job or resource does not replace a running job.
func executionResources(closure map[string]any, project *types.Project,
	files map[string]domain.ComposeSnapshotFile,
) (map[string]any, map[string]domain.ComposeSnapshotFile) {
	resources, inputs := map[string]any{}, map[string]domain.ComposeSnapshotFile{}
	add := func(key string, value any) {
		resources[key] = value
		if data, ok := files[key]; ok {
			inputs[key] = data
		}
	}
	for _, spec := range executionSpecifications(closure) {
		addContainerResources(spec.ContainerSpec, project, add)
		if spec.Build != nil {
			for _, secret := range spec.Build.Secrets {
				add(secretResourcePrefix+secret.Source, project.Secrets[secret.Source])
			}
		}
	}
	return resources, inputs
}

// addContainerResources includes the top-level objects a container references, so their definitions participate
// in revision selection alongside the workload itself.
func addContainerResources(container types.ContainerSpec, project *types.Project, add func(string, any)) {
	for _, config := range container.Configs {
		add("configs/"+config.Source, project.Configs[config.Source])
	}
	for _, secret := range container.Secrets {
		add(secretResourcePrefix+secret.Source, project.Secrets[secret.Source])
	}
	for name := range container.Networks {
		add("networks/"+name, project.Networks[name])
	}
	for _, volume := range container.Volumes {
		if volume.Type == types.VolumeTypeVolume && volume.Source != "" {
			add("volumes/"+volume.Source, project.Volumes[volume.Source])
		}
	}
	for name := range container.Models {
		add("models/"+name, project.Models[name])
	}
}

// executionSpecifications includes service pre-start containers because their independently mounted
// configuration is part of executing the dependency.
func executionSpecifications(closure map[string]any) []types.ServiceConfig {
	var out []types.ServiceConfig
	for _, definition := range closure {
		switch def := definition.(type) {
		case types.JobConfig:
			out = append(out, types.ServiceConfig{ContainerSpec: def.ContainerSpec, WorkloadSpec: def.WorkloadSpec})
		case types.ServiceConfig:
			out = append(out, def)
			for _, hook := range def.PreStart {
				out = append(out, types.ServiceConfig{ContainerSpec: hook.ContainerSpec})
			}
		}
	}
	return out
}

// resourceEnvironment limits captured synthetic values to the referenced resources, preventing unrelated
// environment changes from recreating a service.
func resourceEnvironment(resources map[string]any, environment map[string]string) map[string]string {
	out := map[string]string{}
	for _, resource := range resources {
		var name string
		switch source := resource.(type) {
		case types.ConfigObjConfig:
			name = source.Environment
		case types.SecretConfig:
			name = source.Environment
		}
		if value, ok := environment[name]; ok {
			out[name] = value
		}
	}
	return out
}
