package cli

import (
	"testing"

	"github.com/netresearch/ofelia/test"
)

// TestBuildFromString_WithDependencies tests parsing of job dependency fields from INI config
func TestBuildFromString_WithDependencies(t *testing.T) {
	t.Parallel()
	configStr := `
[job-exec "job-a"]
schedule = @every 5s
container = test-container
command = echo a

[job-exec "job-b"]
schedule = @every 10s
container = test-container
command = echo b
depends-on = job-a

[job-exec "job-c"]
schedule = @every 15s
container = test-container
command = echo c
depends-on = job-a
depends-on = job-b
on-success = job-a
on-failure = job-b
`

	logger := test.NewTestLogger()
	cfg, err := BuildFromString(configStr, logger)
	if err != nil {
		t.Fatalf("BuildFromString failed: %v", err)
	}

	// Verify all jobs were parsed
	if len(cfg.ExecJobs) != 3 {
		t.Fatalf("Expected 3 exec jobs, got %d", len(cfg.ExecJobs))
	}

	// Test job-a (no dependencies)
	jobA, exists := cfg.ExecJobs["job-a"]
	if !exists {
		t.Fatal("job-a not found")
	}
	if len(jobA.Dependencies) != 0 {
		t.Errorf("job-a: expected 0 dependencies, got %d: %v", len(jobA.Dependencies), jobA.Dependencies)
	}

	// Test job-b (single dependency)
	jobB, exists := cfg.ExecJobs["job-b"]
	if !exists {
		t.Fatal("job-b not found")
	}
	if len(jobB.Dependencies) != 1 {
		t.Errorf("job-b: expected 1 dependency, got %d: %v", len(jobB.Dependencies), jobB.Dependencies)
	}
	if len(jobB.Dependencies) > 0 && jobB.Dependencies[0] != "job-a" {
		t.Errorf("job-b: expected dependency 'job-a', got %q", jobB.Dependencies[0])
	}

	// Test job-c (multiple dependencies and triggers)
	jobC, exists := cfg.ExecJobs["job-c"]
	if !exists {
		t.Fatal("job-c not found")
	}
	if len(jobC.Dependencies) != 2 {
		t.Errorf("job-c: expected 2 dependencies, got %d: %v", len(jobC.Dependencies), jobC.Dependencies)
	}
	if len(jobC.OnSuccess) != 1 {
		t.Errorf("job-c: expected 1 on-success trigger, got %d: %v", len(jobC.OnSuccess), jobC.OnSuccess)
	}
	if len(jobC.OnFailure) != 1 {
		t.Errorf("job-c: expected 1 on-failure trigger, got %d: %v", len(jobC.OnFailure), jobC.OnFailure)
	}
	if len(jobC.OnSuccess) > 0 && jobC.OnSuccess[0] != "job-a" {
		t.Errorf("job-c: expected on-success trigger 'job-a', got %q", jobC.OnSuccess[0])
	}
	if len(jobC.OnFailure) > 0 && jobC.OnFailure[0] != "job-b" {
		t.Errorf("job-c: expected on-failure trigger 'job-b', got %q", jobC.OnFailure[0])
	}
}

// TestBuildFromString_DependenciesAllJobTypes tests dependency fields work for all job types
func TestBuildFromString_DependenciesAllJobTypes(t *testing.T) {
	t.Parallel()
	configStr := `
[job-exec "exec-job"]
schedule = @every 5s
container = test-container
command = echo exec
depends-on = base-job
on-success = notify-job

[job-run "run-job"]
schedule = @every 10s
image = alpine
command = echo run
depends-on = base-job
on-failure = cleanup-job

[job-local "local-job"]
schedule = @every 15s
command = echo local
depends-on = setup-job

[job-service-run "service-job"]
schedule = @every 20s
image = nginx
command = echo service
on-success = verify-job
on-failure = rollback-job

[job-compose "compose-job"]
schedule = @every 25s
command = up -d
depends-on = init-job
depends-on = config-job
`

	logger := test.NewTestLogger()
	cfg, err := BuildFromString(configStr, logger)
	if err != nil {
		t.Fatalf("BuildFromString failed: %v", err)
	}

	// Test exec job
	if job, exists := cfg.ExecJobs["exec-job"]; exists {
		if len(job.Dependencies) != 1 || job.Dependencies[0] != "base-job" {
			t.Errorf("exec-job: unexpected dependencies: %v", job.Dependencies)
		}
		if len(job.OnSuccess) != 1 || job.OnSuccess[0] != "notify-job" {
			t.Errorf("exec-job: unexpected on-success: %v", job.OnSuccess)
		}
	} else {
		t.Error("exec-job not found")
	}

	// Test run job
	if job, exists := cfg.RunJobs["run-job"]; exists {
		if len(job.Dependencies) != 1 || job.Dependencies[0] != "base-job" {
			t.Errorf("run-job: unexpected dependencies: %v", job.Dependencies)
		}
		if len(job.OnFailure) != 1 || job.OnFailure[0] != "cleanup-job" {
			t.Errorf("run-job: unexpected on-failure: %v", job.OnFailure)
		}
	} else {
		t.Error("run-job not found")
	}

	// Test local job
	if job, exists := cfg.LocalJobs["local-job"]; exists {
		if len(job.Dependencies) != 1 || job.Dependencies[0] != "setup-job" {
			t.Errorf("local-job: unexpected dependencies: %v", job.Dependencies)
		}
	} else {
		t.Error("local-job not found")
	}

	// Test service job
	if job, exists := cfg.ServiceJobs["service-job"]; exists {
		if len(job.OnSuccess) != 1 || job.OnSuccess[0] != "verify-job" {
			t.Errorf("service-job: unexpected on-success: %v", job.OnSuccess)
		}
		if len(job.OnFailure) != 1 || job.OnFailure[0] != "rollback-job" {
			t.Errorf("service-job: unexpected on-failure: %v", job.OnFailure)
		}
	} else {
		t.Error("service-job not found")
	}

	// Test compose job (multiple dependencies)
	if job, exists := cfg.ComposeJobs["compose-job"]; exists {
		if len(job.Dependencies) != 2 {
			t.Errorf("compose-job: expected 2 dependencies, got %d: %v", len(job.Dependencies), job.Dependencies)
		}
	} else {
		t.Error("compose-job not found")
	}
}

// TestDockerLabels_Dependencies tests parsing of dependency fields from Docker labels
func TestDockerLabels_Dependencies(t *testing.T) {
	t.Parallel()
	labels := map[string]map[string]string{
		"worker-container": {
			"ofelia.enabled":                       "true",
			"ofelia.job-exec.process.schedule":     "@hourly",
			"ofelia.job-exec.process.command":      "process.sh",
			"ofelia.job-exec.process.depends-on":   "setup",
			"ofelia.job-exec.process.on-success":   "cleanup",
			"ofelia.job-exec.process.on-failure":   "alert",
			"ofelia.job-exec.setup.schedule":       "@hourly",
			"ofelia.job-exec.setup.command":        "setup.sh",
			"ofelia.job-exec.cleanup.schedule":     "@triggered",
			"ofelia.job-exec.cleanup.command":      "cleanup.sh",
			"ofelia.job-exec.alert.schedule":       "@triggered",
			"ofelia.job-exec.alert.command":        "alert.sh",
			"ofelia.job-exec.multi-dep.schedule":   "@daily",
			"ofelia.job-exec.multi-dep.command":    "multi.sh",
			"ofelia.job-exec.multi-dep.depends-on": `["setup", "process"]`,
			"ofelia.job-exec.multi-dep.on-success": `["cleanup", "notify"]`,
			"ofelia.job-exec.multi-dep.on-failure": `["alert", "rollback"]`,
		},
	}

	logger := test.NewTestLogger()
	cfg := &Config{logger: logger}
	err := cfg.buildFromDockerLabels(labels)
	if err != nil {
		t.Fatalf("buildFromDockerLabels failed: %v", err)
	}

	// Test single dependency (process job)
	processJob, exists := cfg.ExecJobs["worker-container.process"]
	if !exists {
		t.Fatal("process job not found")
	}
	if len(processJob.Dependencies) != 1 || processJob.Dependencies[0] != "setup" {
		t.Errorf("process job: expected depends-on=['setup'], got %v", processJob.Dependencies)
	}
	if len(processJob.OnSuccess) != 1 || processJob.OnSuccess[0] != "cleanup" {
		t.Errorf("process job: expected on-success=['cleanup'], got %v", processJob.OnSuccess)
	}
	if len(processJob.OnFailure) != 1 || processJob.OnFailure[0] != "alert" {
		t.Errorf("process job: expected on-failure=['alert'], got %v", processJob.OnFailure)
	}

	// Test multiple dependencies (multi-dep job with JSON arrays)
	multiDepJob, exists := cfg.ExecJobs["worker-container.multi-dep"]
	if !exists {
		t.Fatal("multi-dep job not found")
	}
	if len(multiDepJob.Dependencies) != 2 {
		t.Errorf("multi-dep job: expected 2 dependencies, got %d: %v", len(multiDepJob.Dependencies), multiDepJob.Dependencies)
	}
	if len(multiDepJob.OnSuccess) != 2 {
		t.Errorf("multi-dep job: expected 2 on-success triggers, got %d: %v", len(multiDepJob.OnSuccess), multiDepJob.OnSuccess)
	}
	if len(multiDepJob.OnFailure) != 2 {
		t.Errorf("multi-dep job: expected 2 on-failure triggers, got %d: %v", len(multiDepJob.OnFailure), multiDepJob.OnFailure)
	}
}

// TestBuildFromString_EmptyDependencies verifies jobs without dependencies work correctly
func TestBuildFromString_EmptyDependencies(t *testing.T) {
	t.Parallel()
	configStr := `
[job-exec "standalone-job"]
schedule = @every 5s
container = test-container
command = echo standalone
`

	logger := test.NewTestLogger()
	cfg, err := BuildFromString(configStr, logger)
	if err != nil {
		t.Fatalf("BuildFromString failed: %v", err)
	}

	job, exists := cfg.ExecJobs["standalone-job"]
	if !exists {
		t.Fatal("standalone-job not found")
	}

	// All dependency-related fields should be nil or empty
	if len(job.Dependencies) != 0 {
		t.Errorf("Expected no dependencies, got %v", job.Dependencies)
	}
	if len(job.OnSuccess) != 0 {
		t.Errorf("Expected no on-success triggers, got %v", job.OnSuccess)
	}
	if len(job.OnFailure) != 0 {
		t.Errorf("Expected no on-failure triggers, got %v", job.OnFailure)
	}
}

// TestDockerLabels_ComposeServiceName tests that Docker Compose service names are used for job prefixes
func TestDockerLabels_ComposeServiceName(t *testing.T) {
	t.Parallel()
	// Simulate Docker Compose containers with com.docker.compose.service labels
	labels := map[string]map[string]string{
		"myproject-database-1": {
			"ofelia.enabled":                     "true",
			"com.docker.compose.service":         "database",
			"ofelia.job-exec.backup.schedule":    "@daily",
			"ofelia.job-exec.backup.command":     "pg_dump",
			"ofelia.job-exec.cleanup.schedule":   "@triggered",
			"ofelia.job-exec.cleanup.command":    "cleanup.sh",
			"ofelia.job-exec.cleanup.on-success": "notify",
		},
		"myproject-app-1": {
			"ofelia.enabled":                     "true",
			"com.docker.compose.service":         "app",
			"ofelia.job-exec.process.schedule":   "@hourly",
			"ofelia.job-exec.process.command":    "process.sh",
			"ofelia.job-exec.process.depends-on": "database.backup",
		},
	}

	logger := test.NewTestLogger()
	cfg := &Config{logger: logger}
	err := cfg.buildFromDockerLabels(labels)
	if err != nil {
		t.Fatalf("buildFromDockerLabels failed: %v", err)
	}

	// Jobs should be named using service name, not container name
	// database.backup instead of myproject-database-1.backup
	if _, exists := cfg.ExecJobs["database.backup"]; !exists {
		t.Error("Expected job 'database.backup' using Compose service name, not found")
		t.Logf("Available jobs: %v", getJobNames(cfg.ExecJobs))
	}

	if _, exists := cfg.ExecJobs["database.cleanup"]; !exists {
		t.Error("Expected job 'database.cleanup' using Compose service name, not found")
	}

	if _, exists := cfg.ExecJobs["app.process"]; !exists {
		t.Error("Expected job 'app.process' using Compose service name, not found")
	}

	// Verify cross-container references use service names
	if processJob, exists := cfg.ExecJobs["app.process"]; exists {
		if len(processJob.Dependencies) != 1 || processJob.Dependencies[0] != "database.backup" {
			t.Errorf("Expected dependency 'database.backup', got %v", processJob.Dependencies)
		}
	}

	// Container names should NOT be used when Compose service label is present
	if _, exists := cfg.ExecJobs["myproject-database-1.backup"]; exists {
		t.Error("Should NOT use container name when Compose service name is available")
	}
}

// TestDockerLabels_FallbackToContainerName tests fallback to container name when no Compose label
func TestDockerLabels_FallbackToContainerName(t *testing.T) {
	t.Parallel()
	// Non-Compose container (no com.docker.compose.service label)
	labels := map[string]map[string]string{
		"standalone-worker": {
			"ofelia.enabled":                "true",
			"ofelia.job-exec.task.schedule": "@daily",
			"ofelia.job-exec.task.command":  "run-task.sh",
		},
	}

	logger := test.NewTestLogger()
	cfg := &Config{logger: logger}
	err := cfg.buildFromDockerLabels(labels)
	if err != nil {
		t.Fatalf("buildFromDockerLabels failed: %v", err)
	}

	// Should use container name since no Compose service label
	if _, exists := cfg.ExecJobs["standalone-worker.task"]; !exists {
		t.Error("Expected job 'standalone-worker.task' using container name fallback, not found")
		t.Logf("Available jobs: %v", getJobNames(cfg.ExecJobs))
	}
}

// TestDockerLabels_MixedComposeAndNonCompose tests mixed Compose and non-Compose containers
func TestDockerLabels_MixedComposeAndNonCompose(t *testing.T) {
	t.Parallel()
	labels := map[string]map[string]string{
		// Compose container
		"myproject-db-1": {
			"ofelia.enabled":                  "true",
			"com.docker.compose.service":      "db",
			"ofelia.job-exec.backup.schedule": "@daily",
			"ofelia.job-exec.backup.command":  "backup.sh",
		},
		// Non-Compose container
		"legacy-worker": {
			"ofelia.enabled":               "true",
			"ofelia.job-exec.run.schedule": "@hourly",
			"ofelia.job-exec.run.command":  "run.sh",
		},
	}

	logger := test.NewTestLogger()
	cfg := &Config{logger: logger}
	err := cfg.buildFromDockerLabels(labels)
	if err != nil {
		t.Fatalf("buildFromDockerLabels failed: %v", err)
	}

	// Compose container should use service name
	if _, exists := cfg.ExecJobs["db.backup"]; !exists {
		t.Error("Expected 'db.backup' for Compose container")
	}

	// Non-Compose container should use container name
	if _, exists := cfg.ExecJobs["legacy-worker.run"]; !exists {
		t.Error("Expected 'legacy-worker.run' for non-Compose container")
	}
}

// TestDockerLabels_ExplicitContainerOverride tests that an explicit container label
// overrides the default container (fixes #356 / upstream #227)
func TestDockerLabels_ExplicitContainerOverride(t *testing.T) {
	t.Parallel()
	// Container "my_container" defines a job that should run in "backup" container
	// This is the exact scenario from upstream issue #227
	labels := map[string]map[string]string{
		"my_container": {
			"ofelia.enabled":                       "true",
			"ofelia.job-exec.backup-pg1.schedule":  "@every 2m",
			"ofelia.job-exec.backup-pg1.command":   "/backup my_container",
			"ofelia.job-exec.backup-pg1.container": "backup",
		},
	}

	logger := test.NewTestLogger()
	cfg := &Config{logger: logger}
	err := cfg.buildFromDockerLabels(labels)
	if err != nil {
		t.Fatalf("buildFromDockerLabels failed: %v", err)
	}

	// Job should exist with scoped name
	job, exists := cfg.ExecJobs["my_container.backup-pg1"]
	if !exists {
		t.Fatalf("Expected job 'my_container.backup-pg1', not found. Available: %v", getJobNames(cfg.ExecJobs))
	}

	// The container should be "backup" (explicitly specified), NOT "my_container" (where label was defined)
	if job.Container != "backup" {
		t.Errorf("Expected container 'backup' (explicit override), got '%s'", job.Container)
	}

	// Verify the command is correct
	if job.Command != "/backup my_container" {
		t.Errorf("Expected command '/backup my_container', got '%s'", job.Command)
	}
}

// TestDockerLabels_DefaultContainerWhenNotSpecified tests that container defaults to label source
func TestDockerLabels_DefaultContainerWhenNotSpecified(t *testing.T) {
	t.Parallel()
	// Container "web" defines a job without explicit container - should default to "web"
	labels := map[string]map[string]string{
		"web": {
			"ofelia.enabled":                "true",
			"ofelia.job-exec.logs.schedule": "@hourly",
			"ofelia.job-exec.logs.command":  "cat /var/log/app.log",
			// No container label - should default to "web"
		},
	}

	logger := test.NewTestLogger()
	cfg := &Config{logger: logger}
	err := cfg.buildFromDockerLabels(labels)
	if err != nil {
		t.Fatalf("buildFromDockerLabels failed: %v", err)
	}

	job, exists := cfg.ExecJobs["web.logs"]
	if !exists {
		t.Fatalf("Expected job 'web.logs', not found. Available: %v", getJobNames(cfg.ExecJobs))
	}

	// Container should default to "web" since no explicit container was specified
	if job.Container != "web" {
		t.Errorf("Expected container 'web' (default from label source), got '%s'", job.Container)
	}
}

// getJobNames helper to list job names for debugging
func getJobNames[J any](m map[string]J) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	return names
}
