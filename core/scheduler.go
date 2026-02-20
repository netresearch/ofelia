package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/netresearch/go-cron"
)

var (
	ErrEmptyScheduler = errors.New("unable to start an empty scheduler")
	ErrEmptySchedule  = errors.New("unable to add a job with an empty schedule")
)

// TriggeredSchedule is a special schedule keyword for jobs that should only run
// when triggered by another job's on-success/on-failure, or manually via RunJob().
// Jobs with this schedule are not added to the cron scheduler.
const TriggeredSchedule = "@triggered"

// IsTriggeredSchedule returns true if the schedule indicates the job should only
// run when triggered (not on a time-based schedule).
func IsTriggeredSchedule(schedule string) bool {
	return schedule == TriggeredSchedule || schedule == "@manual" || schedule == "@none"
}

type Scheduler struct {
	Jobs     []Job
	Removed  []Job
	Disabled []Job
	Logger   *slog.Logger

	middlewareContainer
	cron                 *cron.Cron
	wg                   sync.WaitGroup
	mu                   sync.RWMutex
	maxConcurrentJobs    int
	jobSemaphore         chan struct{}
	retryExecutor        *RetryExecutor
	workflowOrchestrator *WorkflowOrchestrator
	jobsByName           map[string]Job
	metricsRecorder      MetricsRecorder
	cleanupTicker        Ticker
	cleanupStop          chan struct{}
	clock                Clock
	onJobComplete        func(jobName string, success bool)
}

func NewScheduler(l *slog.Logger) *Scheduler {
	return NewSchedulerWithOptions(l, nil, 0)
}

// NewSchedulerWithMetrics creates a scheduler with metrics (deprecated: use NewSchedulerWithOptions)
func NewSchedulerWithMetrics(l *slog.Logger, metricsRecorder MetricsRecorder) *Scheduler {
	return NewSchedulerWithOptions(l, metricsRecorder, 0)
}

// NewSchedulerWithOptions creates a scheduler with configurable minimum interval.
// minEveryInterval of 0 uses the library default (1s). Use negative value to allow sub-second.
func NewSchedulerWithOptions(l *slog.Logger, metricsRecorder MetricsRecorder, minEveryInterval time.Duration) *Scheduler {
	return newSchedulerInternal(l, metricsRecorder, minEveryInterval, nil)
}

// NewSchedulerWithClock creates a scheduler with a fake clock for testing.
// This allows tests to control time advancement without real waits.
func NewSchedulerWithClock(l *slog.Logger, cronClock *CronClock) *Scheduler {
	return newSchedulerInternal(l, nil, -time.Nanosecond, cronClock)
}

func newSchedulerInternal(
	l *slog.Logger, metricsRecorder MetricsRecorder, minEveryInterval time.Duration, cronClock *CronClock,
) *Scheduler {
	cronUtils := NewCronUtils(l)

	parser := cron.FullParser()
	if minEveryInterval != 0 {
		parser = parser.WithMinEveryInterval(minEveryInterval)
	}

	// Declare cronInstance before hooks so the OnWorkflowComplete closure
	// can capture it by reference; the variable is assigned after cron.New().
	var cronInstance *cron.Cron

	cronOpts := []cron.Option{
		cron.WithParser(parser),
		cron.WithLogger(cronUtils),
		cron.WithChain(cron.Recover(cronUtils)),
		cron.WithCapacity(64), // pre-allocate for typical workloads
	}

	if cronClock != nil {
		cronOpts = append(cronOpts, cron.WithClock(cronClock))
	}

	if metricsRecorder != nil {
		hooks := cron.ObservabilityHooks{
			OnJobStart: func(_ cron.EntryID, name string, _ time.Time) {
				metricsRecorder.RecordJobStart(name)
			},
			OnJobComplete: func(_ cron.EntryID, name string, duration time.Duration, recovered any) {
				metricsRecorder.RecordJobComplete(name, duration.Seconds(), recovered != nil)
			},
			OnSchedule: func(_ cron.EntryID, name string, _ time.Time) {
				metricsRecorder.RecordJobScheduled(name)
			},
			OnWorkflowComplete: func(_ string, rootID cron.EntryID, results map[cron.EntryID]cron.JobResult) {
				recordWorkflowMetrics(cronInstance, metricsRecorder, rootID, results)
			},
		}
		cronOpts = append(cronOpts, cron.WithObservability(hooks))
	}

	cronInstance = cron.New(cronOpts...)

	// Default to 10 concurrent jobs, can be configured
	maxConcurrent := 10

	var clock Clock = GetDefaultClock()
	if cronClock != nil {
		clock = cronClock.FakeClock
	}

	s := &Scheduler{
		Logger:            l,
		cron:              cronInstance,
		maxConcurrentJobs: maxConcurrent,
		jobSemaphore:      make(chan struct{}, maxConcurrent),
		retryExecutor:     NewRetryExecutor(l),
		jobsByName:        make(map[string]Job),
		metricsRecorder:   metricsRecorder,
		clock:             clock,
	}

	// Also set metrics on retry executor
	if metricsRecorder != nil {
		s.retryExecutor.SetMetricsRecorder(metricsRecorder)
	}

	// Initialize workflow orchestrator
	s.workflowOrchestrator = NewWorkflowOrchestrator(s, l)

	// Initialize cleanup channels
	s.cleanupStop = make(chan struct{})

	return s
}

// SetMaxConcurrentJobs configures the maximum number of concurrent jobs
func (s *Scheduler) SetMaxConcurrentJobs(maxJobs int) {
	if maxJobs < 1 {
		maxJobs = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxConcurrentJobs = maxJobs
	s.jobSemaphore = make(chan struct{}, maxJobs)
}

func (s *Scheduler) SetMetricsRecorder(recorder MetricsRecorder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metricsRecorder = recorder
	if s.retryExecutor != nil {
		s.retryExecutor.SetMetricsRecorder(recorder)
	}
}

func (s *Scheduler) SetClock(c Clock) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock = c
}

func (s *Scheduler) SetOnJobComplete(callback func(jobName string, success bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onJobComplete = callback
}

func (s *Scheduler) AddJob(j Job) error {
	return s.AddJobWithTags(j)
}

// AddJobWithTags adds a job with optional tags for categorization.
// Tags can be used to group, filter, and remove related jobs.
// Jobs with @triggered/@manual/@none schedules are stored but not scheduled in cron.
func (s *Scheduler) AddJobWithTags(j Job, tags ...string) error {
	if j.GetSchedule() == "" {
		return ErrEmptySchedule
	}

	// Handle triggered-only jobs: store for manual/workflow execution but don't schedule
	if IsTriggeredSchedule(j.GetSchedule()) {
		j.Use(s.Middlewares()...)
		s.mu.Lock()
		s.Jobs = append(s.Jobs, j)
		s.jobsByName[j.GetName()] = j
		s.mu.Unlock()
		s.Logger.Info(fmt.Sprintf(
			"Triggered-only job registered %q - %q (will run only when triggered)",
			j.GetName(), j.GetCommand(),
		))
		return nil
	}

	// Build job options: always include name for O(1) lookup
	opts := []cron.JobOption{cron.WithName(j.GetName())}
	if len(tags) > 0 {
		opts = append(opts, cron.WithTags(tags...))
	}
	if j.ShouldRunOnStartup() {
		opts = append(opts, cron.WithRunImmediately())
	}

	// Apply global middlewares BEFORE adding to cron, because WithRunImmediately()
	// may cause the job to execute immediately after AddJob returns — before we'd
	// get a chance to apply middlewares afterwards.
	j.Use(s.Middlewares()...)

	id, err := s.cron.AddJob(j.GetSchedule(), &jobWrapper{s, j}, opts...)
	if err != nil {
		s.Logger.Warn(fmt.Sprintf(
			"Failed to register job %q - %q - %q",
			j.GetName(), j.GetCommand(), j.GetSchedule(),
		))
		return fmt.Errorf("add cron job: %w", err)
	}
	j.SetCronJobID(uint64(id))
	s.mu.Lock()
	s.Jobs = append(s.Jobs, j)
	s.jobsByName[j.GetName()] = j
	s.mu.Unlock()
	s.Logger.Info(fmt.Sprintf(
		"New job registered %q - %q - %q - ID: %v",
		j.GetName(), j.GetCommand(), j.GetSchedule(), id,
	))
	return nil
}

func (s *Scheduler) RemoveJob(j Job) error {
	s.Logger.Info(fmt.Sprintf(
		"Job deregistered (will not fire again) %q - %q - %q - ID: %v",
		j.GetName(), j.GetCommand(), j.GetSchedule(), j.GetCronJobID(),
	))
	// Use O(1) removal by name, then wait for any in-flight execution
	// to complete before updating internal state
	s.cron.RemoveByName(j.GetName())
	s.cron.WaitForJobByName(j.GetName())
	s.mu.Lock()
	for i, job := range s.Jobs {
		if job == j || job.GetCronJobID() == j.GetCronJobID() {
			s.Jobs = append(s.Jobs[:i], s.Jobs[i+1:]...)
			break
		}
	}
	delete(s.jobsByName, j.GetName())
	s.Removed = append(s.Removed, j)
	s.mu.Unlock()
	return nil
}

// RemoveJobsByTag removes all jobs with the specified tag.
// Returns the number of jobs removed.
func (s *Scheduler) RemoveJobsByTag(tag string) int {
	// Get entries by tag before removal for logging
	entries := s.cron.EntriesByTag(tag)
	if len(entries) == 0 {
		return 0
	}

	// Remove from cron using O(1) tag removal
	count := s.cron.RemoveByTag(tag)

	// Update our internal state
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range entries {
		// Find and remove from Jobs slice (iterate backwards for safe removal)
		for i := len(s.Jobs) - 1; i >= 0; i-- {
			job := s.Jobs[i]
			if job.GetCronJobID() == uint64(entry.ID) {
				s.Logger.Info(fmt.Sprintf("Job removed by tag %q: %q", tag, job.GetName()))
				delete(s.jobsByName, job.GetName())
				s.Removed = append(s.Removed, job)
				s.Jobs = append(s.Jobs[:i], s.Jobs[i+1:]...)
				break
			}
		}
	}

	return count
}

// GetJobsByTag returns all jobs with the specified tag.
func (s *Scheduler) GetJobsByTag(tag string) []Job {
	entries := s.cron.EntriesByTag(tag)
	if len(entries) == 0 {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]Job, 0, len(entries))
	for _, entry := range entries {
		for _, job := range s.Jobs {
			if job.GetCronJobID() == uint64(entry.ID) {
				jobs = append(jobs, job)
				break
			}
		}
	}
	return jobs
}

func (s *Scheduler) Start() error {
	s.mu.Lock()

	// Build dependency graph
	if err := s.workflowOrchestrator.BuildDependencyGraph(s.Jobs); err != nil {
		s.Logger.Error(fmt.Sprintf("Failed to build dependency graph: %v", err))
		// Continue anyway - jobs without dependencies will still work
	}

	// Build job name lookup map
	for _, j := range s.Jobs {
		s.jobsByName[j.GetName()] = j
	}

	// Start workflow cleanup routine
	s.startWorkflowCleanup()

	// Collect triggered-only jobs that need startup execution while we hold the lock.
	// These jobs are not added to go-cron, so WithRunImmediately() does not apply.
	var startupTriggered []Job
	for _, j := range s.Jobs {
		if IsTriggeredSchedule(j.GetSchedule()) && j.ShouldRunOnStartup() {
			startupTriggered = append(startupTriggered, j)
		}
	}

	s.mu.Unlock()
	s.Logger.Debug("Starting scheduler")
	s.cron.Start()

	// Fire startup execution for triggered-only jobs.
	// Register with wg before spawning to prevent Stop()'s wg.Wait() from
	// completing before goroutines have started.
	s.mu.Lock()
	for range startupTriggered {
		s.wg.Add(1)
	}
	s.mu.Unlock()
	for _, j := range startupTriggered {
		s.Logger.Info(fmt.Sprintf("Running triggered-only job %q on startup", j.GetName()))
		wrapper := &jobWrapper{s: s, j: j}
		go func() {
			defer func() {
				s.mu.Lock()
				s.wg.Done()
				s.mu.Unlock()
			}()
			wrapper.RunWithContext(context.Background())
		}()
	}

	return nil
}

// DefaultStopTimeout is the default timeout for graceful shutdown.
const DefaultStopTimeout = 30 * time.Second

func (s *Scheduler) Stop() error {
	return s.StopWithTimeout(DefaultStopTimeout)
}

// StopWithTimeout stops the scheduler with a graceful shutdown timeout.
// It stops accepting new jobs, then waits up to the timeout for running jobs to complete.
// Returns nil if all jobs completed, or an error if the timeout was exceeded.
func (s *Scheduler) StopWithTimeout(timeout time.Duration) error {
	// Use go-cron's StopWithTimeout for graceful shutdown
	completed := s.cron.StopWithTimeout(timeout)

	s.mu.Lock()

	// Stop cleanup routine
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
		close(s.cleanupStop)
	}
	s.mu.Unlock()

	s.wg.Wait() // Wait for any remaining wrapper goroutines

	if !completed {
		s.Logger.Warn(fmt.Sprintf("Scheduler stop timed out after %v - some jobs may still be running", timeout))
		return fmt.Errorf("%w after %v", ErrSchedulerTimeout, timeout)
	}
	s.Logger.Debug("Scheduler stopped gracefully")
	return nil
}

// StopAndWait stops the scheduler and waits indefinitely for all jobs to complete.
func (s *Scheduler) StopAndWait() {
	s.cron.StopAndWait()

	s.mu.Lock()

	// Stop cleanup routine
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
		close(s.cleanupStop)
	}
	s.mu.Unlock()

	s.wg.Wait()
	s.Logger.Debug("Scheduler stopped and all jobs completed")
}

// startWorkflowCleanup starts the background cleanup routine for workflow executions
func (s *Scheduler) startWorkflowCleanup() {
	// Default cleanup interval: 1 hour
	// Default retention: 24 hours
	cleanupInterval := 1 * time.Hour
	retentionDuration := 24 * time.Hour

	// Check for environment variable overrides
	if interval := os.Getenv("OFELIA_WORKFLOW_CLEANUP_INTERVAL"); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			cleanupInterval = d
		}
	}
	if retention := os.Getenv("OFELIA_WORKFLOW_RETENTION"); retention != "" {
		if d, err := time.ParseDuration(retention); err == nil {
			retentionDuration = d
		}
	}

	s.cleanupTicker = s.clock.NewTicker(cleanupInterval)

	go func() {
		for {
			select {
			case <-s.cleanupTicker.C():
				s.workflowOrchestrator.CleanupOldExecutions(retentionDuration)
				s.Logger.Debug(fmt.Sprintf("Cleaned up workflow executions older than %v", retentionDuration))
			case <-s.cleanupStop:
				return
			}
		}
	}()
}

// Entries returns all scheduled cron entries.
func (s *Scheduler) Entries() []cron.Entry {
	return s.cron.Entries()
}

// RunJob manually triggers a job by name. The provided context is propagated
// to the job's RunWithContext method and is available via Context.Ctx.
func (s *Scheduler) RunJob(ctx context.Context, jobName string) error {
	s.mu.RLock()
	job, exists := s.jobsByName[jobName]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("%w: %s", ErrJobNotFound, jobName)
	}

	executionID := fmt.Sprintf("manual-%d", s.clock.Now().Unix())
	if !s.workflowOrchestrator.CanExecute(jobName, executionID) {
		return fmt.Errorf("%w: %s", ErrDependencyNotMet, jobName)
	}

	wrapper := &jobWrapper{s: s, j: job}
	go wrapper.RunWithContext(ctx)

	return nil
}

// GetRemovedJobs returns a copy of all jobs that were removed from the scheduler.
func (s *Scheduler) GetRemovedJobs() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]Job, len(s.Removed))
	copy(jobs, s.Removed)
	return jobs
}

// GetDisabledJobs returns a copy of all disabled jobs.
func (s *Scheduler) GetDisabledJobs() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]Job, len(s.Disabled))
	copy(jobs, s.Disabled)
	return jobs
}

// getJob finds a job in the provided slice by name.
func getJob(jobs []Job, name string) (Job, int) {
	for i, j := range jobs {
		if j.GetName() == name {
			return j, i
		}
	}
	return nil, -1
}

// GetJob returns an active job by name.
func (s *Scheduler) GetJob(name string) Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, _ := getJob(s.Jobs, name)
	return j
}

// GetDisabledJob returns a disabled job by name.
func (s *Scheduler) GetDisabledJob(name string) Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, _ := getJob(s.Disabled, name)
	return j
}

// UpdateJob atomically replaces the schedule and job implementation for an
// existing named entry using go-cron's UpdateEntryJobByName. The old job's
// in-flight invocations complete before the new schedule takes effect (because
// go-cron serializes entry mutations through the scheduler goroutine).
//
// Returns ErrJobNotFound if no active job with the given name exists.
func (s *Scheduler) UpdateJob(name string, newSchedule string, newJob Job) error {
	s.mu.RLock()
	oldJob, _ := getJob(s.Jobs, name)
	if oldJob == nil {
		s.mu.RUnlock()
		return fmt.Errorf("%w: %q", ErrJobNotFound, name)
	}
	s.mu.RUnlock()

	newJob.Use(s.Middlewares()...)

	if err := s.cron.UpdateEntryJobByName(name, newSchedule, &jobWrapper{s, newJob}); err != nil {
		return fmt.Errorf("update job: %w", err)
	}

	// Update internal state
	s.mu.Lock()
	for i, j := range s.Jobs {
		if j.GetName() == name {
			s.Jobs[i] = newJob
			break
		}
	}
	s.jobsByName[name] = newJob
	s.mu.Unlock()

	s.Logger.Info(fmt.Sprintf("Job updated %q - %q", name, newSchedule))
	return nil
}

// DisableJob stops scheduling the job but keeps it for later enabling.
func (s *Scheduler) DisableJob(name string) error {
	// First, find the job under read lock
	s.mu.RLock()
	j, _ := getJob(s.Jobs, name)
	if j == nil {
		s.mu.RUnlock()
		return fmt.Errorf("%w: %q", ErrJobNotFound, name)
	}
	s.mu.RUnlock()

	// Remove from cron without holding our lock (cron has its own lock)
	// Use RemoveByName to properly clean up the name registry for later re-enabling
	s.cron.RemoveByName(name)
	// Wait for any in-flight execution to complete before updating state
	s.cron.WaitForJobByName(name)

	// Now acquire write lock to update internal state
	s.mu.Lock()
	defer s.mu.Unlock()
	// Re-find the job since state may have changed
	j, idx := getJob(s.Jobs, name)
	if j == nil {
		// Job was already removed by another goroutine
		return nil
	}
	delete(s.jobsByName, name)
	s.Jobs = append(s.Jobs[:idx], s.Jobs[idx+1:]...)
	s.Disabled = append(s.Disabled, j)
	return nil
}

// EnableJob schedules a previously disabled job.
// Uses go-cron's UpsertJob for atomic create-or-update, eliminating the
// previous polling retry loop that waited for RemoveByName to take effect.
func (s *Scheduler) EnableJob(name string) error {
	s.mu.Lock()
	j, idx := getJob(s.Disabled, name)
	if j == nil {
		s.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrJobNotFound, name)
	}
	s.Disabled = append(s.Disabled[:idx], s.Disabled[idx+1:]...)
	s.mu.Unlock()

	if IsTriggeredSchedule(j.GetSchedule()) {
		// Triggered-only jobs aren't scheduled in cron; just restore state
		j.Use(s.Middlewares()...)
		s.mu.Lock()
		s.Jobs = append(s.Jobs, j)
		s.jobsByName[j.GetName()] = j
		s.mu.Unlock()
		return nil
	}

	j.Use(s.Middlewares()...)

	opts := []cron.JobOption{cron.WithName(j.GetName())}
	if j.ShouldRunOnStartup() {
		opts = append(opts, cron.WithRunImmediately())
	}

	id, err := s.cron.UpsertJob(j.GetSchedule(), &jobWrapper{s, j}, opts...)
	if err != nil {
		return fmt.Errorf("enable job: %w", err)
	}
	j.SetCronJobID(uint64(id))

	s.mu.Lock()
	s.Jobs = append(s.Jobs, j)
	s.jobsByName[j.GetName()] = j
	s.mu.Unlock()

	s.Logger.Info(fmt.Sprintf("Job re-enabled %q - %q - ID: %v", j.GetName(), j.GetSchedule(), id))
	return nil
}

// unknownJobName is used when a cron entry's name cannot be resolved from its ID.
const unknownJobName = "unknown"

// Workflow status constants returned by workflowStatus.
const (
	workflowStatusSuccess = "success"
	workflowStatusFailure = "failure"
	workflowStatusSkipped = "skipped"
	workflowStatusMixed   = "mixed"
)

// recordWorkflowMetrics extracts job names from cron entries and records
// workflow completion and per-job result metrics. It is called from the
// OnWorkflowComplete observability hook.
func recordWorkflowMetrics(
	cronInstance *cron.Cron,
	recorder MetricsRecorder,
	rootID cron.EntryID,
	results map[cron.EntryID]cron.JobResult,
) {
	// Determine the root job name from its entry
	rootName := unknownJobName
	if rootEntry := cronInstance.Entry(rootID); rootEntry.Name != "" {
		rootName = rootEntry.Name
	}

	// Aggregate results to determine overall workflow status
	status := workflowStatus(results)
	recorder.RecordWorkflowComplete(rootName, status)

	// Record individual job results
	for entryID, result := range results {
		jobName := unknownJobName
		if entry := cronInstance.Entry(entryID); entry.Name != "" {
			jobName = entry.Name
		}
		recorder.RecordWorkflowJobResult(jobName, result.String())
	}
}

// workflowStatus computes an aggregate status string from the per-job results map.
// Returns "success" if all jobs succeeded, "failure" if any failed, "skipped" if
// all non-pending results are skipped, or "mixed" for other combinations.
// An empty or nil map returns "success" (vacuously true).
func workflowStatus(results map[cron.EntryID]cron.JobResult) string {
	if len(results) == 0 {
		return workflowStatusSuccess
	}

	hasFailure := false
	hasSuccess := false
	hasSkipped := false

	for _, r := range results {
		switch r {
		case cron.ResultFailure:
			hasFailure = true
		case cron.ResultSuccess:
			hasSuccess = true
		case cron.ResultSkipped:
			hasSkipped = true
		case cron.ResultPending:
			// Pending jobs are not terminal; should not appear in
			// OnWorkflowComplete results, but handle gracefully.
		}
	}

	switch {
	case hasFailure:
		return workflowStatusFailure
	case hasSuccess && !hasSkipped:
		return workflowStatusSuccess
	case hasSkipped && !hasSuccess:
		return workflowStatusSkipped
	default:
		// Covers success+skipped combinations, pending-only (shouldn't occur),
		// and any future JobResult values not yet handled.
		return workflowStatusMixed
	}
}

// jobWrapper wraps a Job to manage running and waiting via the Scheduler.

// IsRunning returns true if the scheduler is active.
// Delegates to go-cron's IsRunning() which is the authoritative source.
func (s *Scheduler) IsRunning() bool {
	return s.cron.IsRunning()
}

// IsJobRunning reports whether the named job has any invocations currently in
// flight. Returns false if no job with the given name exists or the scheduler
// has no cron instance.
func (s *Scheduler) IsJobRunning(name string) bool {
	if s.cron == nil {
		return false
	}
	return s.cron.IsJobRunningByName(name)
}

type jobWrapper struct {
	s *Scheduler
	j Job
}

// Compile-time assertion: jobWrapper implements cron.JobWithContext.
var _ cron.JobWithContext = (*jobWrapper)(nil)

// Run implements cron.Job. Called by go-cron for jobs that don't support context.
func (w *jobWrapper) Run() {
	w.runWithCtx(context.Background())
}

// RunWithContext implements cron.JobWithContext. Called by go-cron with a
// per-entry context that is canceled when the entry is removed or replaced.
func (w *jobWrapper) RunWithContext(ctx context.Context) {
	w.runWithCtx(ctx)
}

func (w *jobWrapper) runWithCtx(ctx context.Context) {
	// Add panic recovery to handle job panics gracefully
	defer func() {
		if r := recover(); r != nil {
			w.s.Logger.Error("Job panicked", "job", w.j.GetName(), "recover", r)
		}
	}()

	executionID := fmt.Sprintf("sched-%d-%s", w.s.clock.Now().Unix(), w.j.GetName())

	// Check dependencies
	if !w.s.workflowOrchestrator.CanExecute(w.j.GetName(), executionID) {
		w.s.Logger.Debug(fmt.Sprintf("Job %q skipped - dependencies not satisfied", w.j.GetName()))
		return
	}

	// Acquire semaphore slot for job concurrency limit
	select {
	case w.s.jobSemaphore <- struct{}{}:
		// Got a slot, proceed
		defer func() { <-w.s.jobSemaphore }() // Release slot when done
	default:
		// No slots available, skip this execution
		w.s.Logger.Warn(fmt.Sprintf("Job %q skipped - max concurrent jobs limit reached (%d)",
			w.j.GetName(), w.s.maxConcurrentJobs))
		return
	}

	w.s.mu.Lock()
	if !w.s.cron.IsRunning() {
		w.s.mu.Unlock()
		return
	}
	w.s.wg.Add(1)
	w.s.mu.Unlock()

	defer func() {
		w.s.mu.Lock()
		w.s.wg.Done()
		w.s.mu.Unlock()
	}()

	e, err := NewExecution()
	if err != nil {
		w.s.Logger.Error(fmt.Sprintf("failed to create execution: %v", err))
		return
	}

	// Ensure buffers are returned to pool when done
	defer e.Cleanup()

	jctx := NewContextWithContext(ctx, w.s, w.j, e)

	// Mark job as started in workflow
	w.s.workflowOrchestrator.JobStarted(w.j.GetName(), executionID)

	w.start(jctx)

	// Execute with retry logic
	err = w.s.retryExecutor.ExecuteWithRetry(w.j, jctx, func(c *Context) error {
		return c.Next()
	})

	w.stop(jctx, err)

	success := err == nil && !jctx.Execution.Failed
	w.s.workflowOrchestrator.JobCompleted(ctx, w.j.GetName(), executionID, success)

	if w.s.onJobComplete != nil {
		w.s.onJobComplete(w.j.GetName(), success)
	}
}

func (w *jobWrapper) start(ctx *Context) {
	ctx.Start()
	ctx.Log("Started - " + ctx.Job.GetCommand())

	// Record job started metric if available
	// Note: Job start metrics could be recorded here when metricsRecorder is available
	// Currently focusing on retry metrics (recorded elsewhere)
}

func (w *jobWrapper) stop(ctx *Context, err error) {
	ctx.Stop(err)

	if l, ok := ctx.Job.(interface{ SetLastRun(*Execution) }); ok {
		l.SetLastRun(ctx.Execution)
	}

	errText := "none"
	if ctx.Execution.Error != nil {
		errText = ctx.Execution.Error.Error()
	}

	if ctx.Execution.OutputStream.TotalWritten() > 0 {
		ctx.Log("StdOut: " + ctx.Execution.OutputStream.String())
	}

	if ctx.Execution.ErrorStream.TotalWritten() > 0 {
		ctx.Log("StdErr: " + ctx.Execution.ErrorStream.String())
	}

	msg := fmt.Sprintf(
		"Finished in %q, failed: %t, skipped: %t, error: %s",
		ctx.Execution.Duration, ctx.Execution.Failed, ctx.Execution.Skipped, errText,
	)

	ctx.Log(msg)
}
