//go:build e2e
// +build e2e

package e2e

import (
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/netresearch/ofelia/core"
	"github.com/sirupsen/logrus"
)

// TestScheduler_BasicLifecycle tests the complete scheduler lifecycle:
// 1. Start scheduler with config
// 2. Verify jobs are scheduled
// 3. Wait for job execution
// 4. Verify job ran successfully
// 5. Stop scheduler gracefully
func TestScheduler_BasicLifecycle(t *testing.T) {
	// Connect to Docker
	client, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		t.Skip("Docker not available, skipping E2E test")
	}

	// Verify Docker is reachable
	if _, err := client.Info(); err != nil {
		t.Skipf("Docker daemon not reachable: %v", err)
	}

	// Create a test container that stays running
	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Name: "ofelia-e2e-test-container",
		Config: &docker.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "300"},
		},
	})
	if err != nil {
		t.Skipf("Failed to create test container: %v", err)
	}
	defer cleanupContainer(t, client, container.ID)

	// Start the container
	err = client.StartContainer(container.ID, nil)
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}

	// Create scheduler with logger
	logger := &core.LogrusAdapter{Logger: logrus.New()}
	scheduler := core.NewScheduler(logger)

	// Create and add job
	job := &core.ExecJob{
		BareJob: core.BareJob{
			Name:     "test-exec-job",
			Schedule: "@every 2s",
			Command:  "echo E2E test executed",
		},
		Client:    client,
		Container: container.ID,
	}
	job.InitializeRuntimeFields()

	if err := scheduler.AddJob(job); err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Start scheduler in background
	errChan := make(chan error, 1)
	go func() {
		errChan <- scheduler.Start()
	}()

	// Give scheduler time to start and execute job at least once
	time.Sleep(5 * time.Second)

	// Stop scheduler
	scheduler.Stop()

	// Wait for scheduler to finish
	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Scheduler returned error: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Error("Scheduler did not stop within timeout")
	}

	// Verify job executed by checking history
	// Safe to access scheduler.Jobs after Stop() completes and errChan signals,
	// as all scheduler goroutines have exited
	jobs := scheduler.Jobs
	if len(jobs) == 0 {
		t.Fatal("No jobs found in scheduler")
	}

	executedJob := jobs[0]
	history := executedJob.GetHistory()
	if len(history) == 0 {
		t.Error("Job did not execute (no history entries)")
	} else {
		t.Logf("Job executed %d time(s)", len(history))
		lastExec := history[len(history)-1]
		if lastExec.Failed {
			t.Errorf("Last execution failed with error: %v", lastExec.Error)
		}
	}
}

// TestScheduler_MultipleJobsConcurrent tests concurrent execution of multiple jobs
func TestScheduler_MultipleJobsConcurrent(t *testing.T) {
	client, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		t.Skip("Docker not available, skipping E2E test")
	}

	// Create test container
	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Name: "ofelia-e2e-multi-test",
		Config: &docker.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "300"},
		},
	})
	if err != nil {
		t.Skipf("Failed to create test container: %v", err)
	}
	defer cleanupContainer(t, client, container.ID)

	err = client.StartContainer(container.ID, nil)
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}

	// Create scheduler with logger
	logger := &core.LogrusAdapter{Logger: logrus.New()}
	scheduler := core.NewScheduler(logger)

	jobs := []*core.ExecJob{
		{
			BareJob: core.BareJob{
				Name:          "job-1",
				Schedule:      "@every 1s",
				Command:       "echo job1",
				AllowParallel: true,
			},
			Client:    client,
			Container: container.ID,
		},
		{
			BareJob: core.BareJob{
				Name:          "job-2",
				Schedule:      "@every 1s",
				Command:       "echo job2",
				AllowParallel: true,
			},
			Client:    client,
			Container: container.ID,
		},
		{
			BareJob: core.BareJob{
				Name:          "job-3",
				Schedule:      "@every 1s",
				Command:       "echo job3",
				AllowParallel: true,
			},
			Client:    client,
			Container: container.ID,
		},
	}

	for _, job := range jobs {
		job.InitializeRuntimeFields()
		if err := scheduler.AddJob(job); err != nil {
			t.Fatalf("Failed to add job: %v", err)
		}
	}

	// Start scheduler
	errChan := make(chan error, 1)
	go func() {
		errChan <- scheduler.Start()
	}()

	// Let jobs run
	time.Sleep(5 * time.Second)

	// Stop scheduler
	scheduler.Stop()

	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Scheduler returned error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Error("Scheduler did not stop within timeout")
	}

	// Verify all jobs executed
	// Safe to access scheduler.Jobs after Stop() completes and errChan signals
	schedulerJobs := scheduler.Jobs
	if len(schedulerJobs) != 3 {
		t.Fatalf("Expected 3 jobs, got %d", len(schedulerJobs))
	}

	for _, job := range schedulerJobs {
		history := job.GetHistory()
		if len(history) == 0 {
			t.Errorf("Job %s did not execute", job.GetName())
		} else {
			t.Logf("Job %s executed %d time(s)", job.GetName(), len(history))
		}
	}
}

// TestScheduler_JobFailureHandling tests how scheduler handles job failures
func TestScheduler_JobFailureHandling(t *testing.T) {
	client, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		t.Skip("Docker not available, skipping E2E test")
	}

	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Name: "ofelia-e2e-failure-test",
		Config: &docker.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "300"},
		},
	})
	if err != nil {
		t.Skipf("Failed to create test container: %v", err)
	}
	defer cleanupContainer(t, client, container.ID)

	err = client.StartContainer(container.ID, nil)
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}

	// Create scheduler with logger
	logger := &core.LogrusAdapter{Logger: logrus.New()}
	scheduler := core.NewScheduler(logger)

	failingJob := &core.ExecJob{
		BareJob: core.BareJob{
			Name:     "failing-job",
			Schedule: "@every 2s",
			Command:  "false", // Always fails
		},
		Client:    client,
		Container: container.ID,
	}
	failingJob.InitializeRuntimeFields()

	if err := scheduler.AddJob(failingJob); err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Start scheduler
	errChan := make(chan error, 1)
	go func() {
		errChan <- scheduler.Start()
	}()

	// Let job fail a few times
	time.Sleep(5 * time.Second)

	// Stop scheduler
	scheduler.Stop()

	select {
	case <-errChan:
		// Scheduler should not crash even with failing jobs
	case <-time.After(10 * time.Second):
		t.Error("Scheduler did not stop within timeout")
	}

	// Verify job executed but failed
	// Safe to access scheduler.Jobs after Stop() completes and errChan signals
	jobs := scheduler.Jobs
	if len(jobs) == 0 {
		t.Fatal("No jobs found in scheduler")
	}

	failedJob := jobs[0]
	history := failedJob.GetHistory()
	if len(history) == 0 {
		t.Error("Failing job did not execute")
	} else {
		lastExec := history[len(history)-1]
		if !lastExec.Failed {
			t.Error("Expected job to fail, but it succeeded")
		}
		if lastExec.Error == nil {
			t.Error("Expected error for failing job, but got nil")
		}
		t.Logf("Job correctly failed with error: %v", lastExec.Error)
	}
}

// Helper function to cleanup containers
func cleanupContainer(t *testing.T, client *docker.Client, containerID string) {
	err := client.RemoveContainer(docker.RemoveContainerOptions{
		ID:    containerID,
		Force: true,
	})
	if err != nil {
		t.Logf("Warning: Failed to remove container %s: %v", containerID, err)
	}
}
