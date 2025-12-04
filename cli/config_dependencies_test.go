package cli

import (
	"testing"

	"github.com/netresearch/ofelia/test"
)

// TestBuildFromString_WithDependencies tests parsing of job dependency fields from INI config
func TestBuildFromString_WithDependencies(t *testing.T) {
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

// TestBuildFromString_EmptyDependencies verifies jobs without dependencies work correctly
func TestBuildFromString_EmptyDependencies(t *testing.T) {
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
	if job.Dependencies != nil && len(job.Dependencies) != 0 {
		t.Errorf("Expected no dependencies, got %v", job.Dependencies)
	}
	if job.OnSuccess != nil && len(job.OnSuccess) != 0 {
		t.Errorf("Expected no on-success triggers, got %v", job.OnSuccess)
	}
	if job.OnFailure != nil && len(job.OnFailure) != 0 {
		t.Errorf("Expected no on-failure triggers, got %v", job.OnFailure)
	}
}
