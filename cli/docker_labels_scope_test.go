// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"strings"
	"testing"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// twoStacksSharingServiceName returns two containers from independent Docker
// Compose projects (acme, globex) deployed from the same template: both expose
// the Compose service name "web" and carry an identical
// ofelia.job-exec.sync-news label. Under the default service-name scoping both
// collapse to the single key "web.sync-news" — the silent-drop collision
// reported in https://github.com/netresearch/ofelia/issues/734.
func twoStacksSharingServiceName() []DockerContainerInfo {
	return []DockerContainerInfo{
		{
			Name:  "acme-web",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                     "true",
				"com.docker.compose.service":         "web",
				"ofelia.job-exec.sync-news.schedule": "@every 5m",
				"ofelia.job-exec.sync-news.command":  "sync-news --tenant acme",
			},
		},
		{
			Name:  "globex-web",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                     "true",
				"com.docker.compose.service":         "web",
				"ofelia.job-exec.sync-news.schedule": "@every 5m",
				"ofelia.job-exec.sync-news.command":  "sync-news --tenant globex",
			},
		},
	}
}

// TestDockerLabels_JobExecLabelScope is the regression test for issue #734.
// It pins the collision behavior of every job-exec-label-scope mode:
//   - "service" (the default) still collapses same-named services to one key,
//     preserving the cross-container depends-on reference contract;
//   - "container" and "container-service" keep the two stacks' jobs distinct;
//   - an unknown value degrades gracefully to the service default rather than
//     panicking or dropping every job.
func TestDockerLabels_JobExecLabelScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		scope     string
		wantJobs  []string
		wantCount int
	}{
		{
			// Empty == the "service" default applied by NewConfig. Both stacks
			// collapse to one key: this is the documented #734 collision and
			// must stay unchanged so existing depends-on references resolve.
			name:      "default (empty) service scope collides by design",
			scope:     "",
			wantJobs:  []string{"web.sync-news"},
			wantCount: 1,
		},
		{
			name:      "explicit service scope collides by design",
			scope:     "service",
			wantJobs:  []string{"web.sync-news"},
			wantCount: 1,
		},
		{
			name:      "container scope keeps both stacks' jobs",
			scope:     "container",
			wantJobs:  []string{"acme-web.sync-news", "globex-web.sync-news"},
			wantCount: 2,
		},
		{
			name:      "container-service scope keeps both stacks' jobs",
			scope:     "container-service",
			wantJobs:  []string{"acme-web.web.sync-news", "globex-web.web.sync-news"},
			wantCount: 2,
		},
		{
			// An unrecognized value must not silently drop every job; it falls
			// back to the safe service default.
			name:      "unknown scope falls back to service default",
			scope:     "bogus",
			wantJobs:  []string{"web.sync-news"},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{logger: test.NewTestLogger()}
			cfg.Global.JobExecLabelScope = tt.scope

			if err := cfg.buildFromDockerContainers(twoStacksSharingServiceName()); err != nil {
				t.Fatalf("buildFromDockerContainers failed: %v", err)
			}

			if len(cfg.ExecJobs) != tt.wantCount {
				t.Errorf("scope %q: expected %d exec job(s), got %d: %v",
					tt.scope, tt.wantCount, len(cfg.ExecJobs), getJobNames(cfg.ExecJobs))
			}
			for _, name := range tt.wantJobs {
				if _, ok := cfg.ExecJobs[name]; !ok {
					t.Errorf("scope %q: expected job %q, not found. Available: %v",
						tt.scope, name, getJobNames(cfg.ExecJobs))
				}
			}
		})
	}
}

// TestDockerLabels_ContainerServiceScope_NonComposeFallback verifies that the
// container-service scope degrades to container-only naming when a container
// has no Compose service label, avoiding an empty "container..job" segment.
func TestDockerLabels_ContainerServiceScope_NonComposeFallback(t *testing.T) {
	t.Parallel()
	cfg := &Config{logger: test.NewTestLogger()}
	cfg.Global.JobExecLabelScope = "container-service"

	standalone := DockerContainerInfo{
		Name:  "standalone-worker",
		State: domain.ContainerState{Running: true},
		Labels: map[string]string{
			"ofelia.enabled":                "true",
			"ofelia.job-exec.task.schedule": "@daily",
			"ofelia.job-exec.task.command":  "run-task.sh",
		},
	}
	if err := cfg.buildFromDockerContainers([]DockerContainerInfo{standalone}); err != nil {
		t.Fatalf("buildFromDockerContainers failed: %v", err)
	}
	if _, ok := cfg.ExecJobs["standalone-worker.task"]; !ok {
		t.Errorf("expected 'standalone-worker.task' (container-service fallback to container name), got %v",
			getJobNames(cfg.ExecJobs))
	}
}

// TestDockerLabels_ContainerScope_PreservesCrossContainerRefs verifies that the
// collision-safe container scope still wires up depends-on references, as long
// as the reference uses the same container-scoped name.
func TestDockerLabels_ContainerScope_PreservesCrossContainerRefs(t *testing.T) {
	t.Parallel()
	cfg := &Config{logger: test.NewTestLogger()}
	cfg.Global.JobExecLabelScope = "container"

	db := DockerContainerInfo{
		Name:  "acme-database",
		State: domain.ContainerState{Running: true},
		Labels: map[string]string{
			"ofelia.enabled":                  "true",
			"com.docker.compose.service":      "database",
			"ofelia.job-exec.backup.schedule": "@daily",
			"ofelia.job-exec.backup.command":  "pg_dump",
		},
	}
	app := DockerContainerInfo{
		Name:  "acme-app",
		State: domain.ContainerState{Running: true},
		Labels: map[string]string{
			"ofelia.enabled":                     "true",
			"com.docker.compose.service":         "app",
			"ofelia.job-exec.process.schedule":   "@hourly",
			"ofelia.job-exec.process.command":    "process.sh",
			"ofelia.job-exec.process.depends-on": "acme-database.backup",
		},
	}
	if err := cfg.buildFromDockerContainers([]DockerContainerInfo{db, app}); err != nil {
		t.Fatalf("buildFromDockerContainers failed: %v", err)
	}

	if _, ok := cfg.ExecJobs["acme-database.backup"]; !ok {
		t.Errorf("expected container-scoped 'acme-database.backup', got %v", getJobNames(cfg.ExecJobs))
	}
	proc, ok := cfg.ExecJobs["acme-app.process"]
	if !ok {
		t.Fatalf("expected container-scoped 'acme-app.process', got %v", getJobNames(cfg.ExecJobs))
	}
	if len(proc.Dependencies) != 1 || proc.Dependencies[0] != "acme-database.backup" {
		t.Errorf("expected dependency 'acme-database.backup', got %v", proc.Dependencies)
	}
}

// TestBuildFromString_JobExecLabelScope verifies the [global]
// job-exec-label-scope option is parsed from INI into Config.Global and that
// NewConfig applies the "service" default when the key is omitted.
func TestBuildFromString_JobExecLabelScope(t *testing.T) {
	t.Parallel()

	if def := NewConfig(test.NewTestLogger()).Global.JobExecLabelScope; def != "service" {
		t.Errorf(`expected default job-exec-label-scope "service", got %q`, def)
	}

	cfg, err := BuildFromString("[global]\njob-exec-label-scope = container\n", test.NewTestLogger())
	if err != nil {
		t.Fatalf("BuildFromString failed: %v", err)
	}
	if cfg.Global.JobExecLabelScope != "container" {
		t.Errorf(`expected job-exec-label-scope "container", got %q`, cfg.Global.JobExecLabelScope)
	}
}

// TestDockerLabels_ServiceScopeCollision_LogsWarning is the regression guard for
// the *silent* part of #734: under service scope a cross-container name
// collision drops one job, and that must be logged loudly — naming both
// containers and the remedy — rather than vanishing without a trace.
func TestDockerLabels_ServiceScopeCollision_LogsWarning(t *testing.T) {
	t.Parallel()
	logger, h := test.NewTestLoggerWithHandler()
	cfg := &Config{logger: logger} // empty scope == "service" default

	if err := cfg.buildFromDockerContainers(twoStacksSharingServiceName()); err != nil {
		t.Fatalf("buildFromDockerContainers failed: %v", err)
	}

	msg := firstWarningContaining(t, h, "name collision")
	for _, want := range []string{"web.sync-news", "acme-web", "globex-web", "job-exec-label-scope"} {
		if !strings.Contains(msg, want) {
			t.Errorf("collision warning is missing %q; got: %s", want, msg)
		}
	}
	// Pin the deterministic owner/dropped roles: sortContainers orders running
	// containers by name, so "acme-web" is scanned first (the surviving owner)
	// and "globex-web" is the dropped duplicate. This guards against an arg-swap
	// regression that would tell the operator the wrong container's job is lost.
	if want := `"globex-web" is dropped because container "acme-web"`; !strings.Contains(msg, want) {
		t.Errorf("collision warning should name globex-web as dropped and acme-web as owner; got: %s", msg)
	}
}

// TestDockerLabels_ContainerScope_NoCollisionWarning ensures the collision
// detector does not fire when the configured scope already keeps the two
// stacks' jobs distinct (no false positives).
func TestDockerLabels_ContainerScope_NoCollisionWarning(t *testing.T) {
	t.Parallel()
	logger, h := test.NewTestLoggerWithHandler()
	cfg := &Config{logger: logger}
	cfg.Global.JobExecLabelScope = "container"

	if err := cfg.buildFromDockerContainers(twoStacksSharingServiceName()); err != nil {
		t.Fatalf("buildFromDockerContainers failed: %v", err)
	}
	if h.HasWarning("name collision") {
		t.Errorf("did not expect a collision warning under container scope; messages: %v", h.GetMessages())
	}
}

// TestDockerLabels_SingleContainer_NoCollisionWarning ensures a single container
// defining several distinct exec jobs never trips the cross-container detector.
func TestDockerLabels_SingleContainer_NoCollisionWarning(t *testing.T) {
	t.Parallel()
	logger, h := test.NewTestLoggerWithHandler()
	cfg := &Config{logger: logger}

	one := DockerContainerInfo{
		Name:  "acme-web",
		State: domain.ContainerState{Running: true},
		Labels: map[string]string{
			"ofelia.enabled":                     "true",
			"com.docker.compose.service":         "web",
			"ofelia.job-exec.sync-news.schedule": "@every 5m",
			"ofelia.job-exec.sync-news.command":  "sync-news",
			"ofelia.job-exec.purge.schedule":     "@daily",
			"ofelia.job-exec.purge.command":      "purge",
		},
	}
	if err := cfg.buildFromDockerContainers([]DockerContainerInfo{one}); err != nil {
		t.Fatalf("buildFromDockerContainers failed: %v", err)
	}
	if h.HasWarning("name collision") {
		t.Errorf("did not expect a collision warning for a single container; messages: %v", h.GetMessages())
	}
}

// TestDockerLabels_UnrecognizedScope_LogsWarning guards against a silent
// failure on this PR's own config surface: a typo'd job-exec-label-scope value
// falls back to the collision-prone "service" default, so the misconfiguration
// must be reported loudly rather than degrading without a trace.
func TestDockerLabels_UnrecognizedScope_LogsWarning(t *testing.T) {
	t.Parallel()
	logger, h := test.NewTestLoggerWithHandler()
	cfg := &Config{logger: logger}
	cfg.Global.JobExecLabelScope = "continer" // typo for "container"

	if err := cfg.buildFromDockerContainers(twoStacksSharingServiceName()); err != nil {
		t.Fatalf("buildFromDockerContainers failed: %v", err)
	}

	msg := firstWarningContaining(t, h, "unrecognized")
	for _, want := range []string{"continer", "job-exec-label-scope", "service", "container", "container-service"} {
		if !strings.Contains(msg, want) {
			t.Errorf("unrecognized-scope warning is missing %q; got: %s", want, msg)
		}
	}
}

// TestDockerLabels_MultipleCollisions_WarnsPerDroppedJob pins warning
// cardinality: the detector emits exactly one warning per dropped job, not a
// single coalesced warning and not one per scan. Three stacks share the same
// service + job name, so one survives and two are dropped.
func TestDockerLabels_MultipleCollisions_WarnsPerDroppedJob(t *testing.T) {
	t.Parallel()
	logger, h := test.NewTestLoggerWithHandler()
	cfg := &Config{logger: logger} // service default

	mk := func(name string) DockerContainerInfo {
		return DockerContainerInfo{
			Name:  name,
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                     "true",
				"com.docker.compose.service":         "web",
				"ofelia.job-exec.sync-news.schedule": "@every 5m",
				"ofelia.job-exec.sync-news.command":  "sync-news --tenant " + name,
			},
		}
	}
	containers := []DockerContainerInfo{mk("acme-web"), mk("globex-web"), mk("initech-web")}
	if err := cfg.buildFromDockerContainers(containers); err != nil {
		t.Fatalf("buildFromDockerContainers failed: %v", err)
	}

	if len(cfg.ExecJobs) != 1 {
		t.Errorf("expected 1 surviving job, got %d: %v", len(cfg.ExecJobs), getJobNames(cfg.ExecJobs))
	}
	collisions := 0
	for _, e := range h.GetMessages() {
		if e.Level == "WARN" && strings.Contains(e.Message, "name collision") {
			collisions++
		}
	}
	if collisions != 2 {
		t.Errorf("expected exactly 2 collision warnings (one per dropped job), got %d: %v", collisions, h.GetMessages())
	}
}

// TestDockerLabels_Collision_NilLoggerDoesNotPanic ensures the collision and
// scope-warning paths degrade gracefully when no logger is configured, matching
// the nil-guarded sibling label warnings rather than panicking on the very
// condition they exist to report.
func TestDockerLabels_Collision_NilLoggerDoesNotPanic(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("collision path panicked with a nil logger: %v", r)
		}
	}()
	cfg := &Config{} // deliberately no logger
	if err := cfg.buildFromDockerContainers(twoStacksSharingServiceName()); err != nil {
		t.Fatalf("buildFromDockerContainers failed: %v", err)
	}
}

// firstWarningContaining returns the first WARN-level message containing substr,
// failing the test if none is found.
func firstWarningContaining(t *testing.T, h *test.Handler, substr string) string {
	t.Helper()
	for _, e := range h.GetMessages() {
		if e.Level == "WARN" && strings.Contains(e.Message, substr) {
			return e.Message
		}
	}
	t.Fatalf("expected a WARN message containing %q; messages: %v", substr, h.GetMessages())
	return ""
}
