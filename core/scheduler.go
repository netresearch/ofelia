package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
// go-cron natively handles these schedules: the parser recognizes @triggered,
// @manual, and @none and creates a TriggeredSchedule whose Next() always returns
// zero time, so the scheduler never fires them automatically. Jobs can still be
// triggered on demand via TriggerEntryByName().
const TriggeredSchedule = "@triggered"

// IsTriggeredSchedule returns true if the schedule string indicates the job
// should only run when triggered (not on a time-based schedule). This is a
// convenience wrapper around string comparison for the three recognized keywords.
func IsTriggeredSchedule(schedule string) bool {
	return schedule == TriggeredSchedule || schedule == "@manual" || schedule == "@none"
}

type Scheduler struct {
	Jobs    []Job
	Removed []Job
	Logger  *slog.Logger

	middlewareContainer
	cron              *cron.Cron
	wg                sync.WaitGroup
	mu                sync.RWMutex
	maxConcurrentJobs int
	concurrencySem    *concurrencySemaphore // go-cron middleware semaphore
	retryExecutor     *RetryExecutor
	jobsByName        map[string]Job
	disabledNames     map[string]struct{}
	metricsRecorder   MetricsRecorder
	clock             Clock
	onJobComplete     func(jobName string, success bool)
}

// concurrencySemaphore holds a swappable semaphore channel used by the
// go-cron MaxConcurrentSkip-style job wrapper. The wrapper reads the
// current channel via a mutex-protected accessor so that SetMaxConcurrentJobs
// can resize the limit before the scheduler is started.
type concurrencySemaphore struct {
	mu  sync.RWMutex
	ch  chan struct{}
	cap int
}

func newConcurrencySemaphore(n int) *concurrencySemaphore {
	return &concurrencySemaphore{
		ch:  make(chan struct{}, n),
		cap: n,
	}
}

func (cs *concurrencySemaphore) resize(n int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.ch = make(chan struct{}, n)
	cs.cap = n
}

func (cs *concurrencySemaphore) getChan() chan struct{} {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.ch
}

func (cs *concurrencySemaphore) getCap() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.cap
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

	// Default to 10 concurrent jobs, can be configured via SetMaxConcurrentJobs
	maxConcurrent := 10
	sem := newConcurrencySemaphore(maxConcurrent)

	// Build the go-cron middleware chain. Concurrency limiting uses a
	// MaxConcurrentSkip-style wrapper backed by the scheduler's resizable
	// semaphore so that SetMaxConcurrentJobs can adjust the limit before Start.
	concurrencyWrapper := maxConcurrentSkipWrapper(cronUtils, sem)

	cronOpts := []cron.Option{
		cron.WithParser(parser),
		cron.WithLogger(cronUtils),
		cron.WithChain(cron.Recover(cronUtils), concurrencyWrapper),
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

	var clock Clock = GetDefaultClock()
	if cronClock != nil {
		clock = cronClock.FakeClock
	}

	s := &Scheduler{
		Logger:            l,
		cron:              cronInstance,
		maxConcurrentJobs: maxConcurrent,
		concurrencySem:    sem,
		retryExecutor:     NewRetryExecutor(l),
		jobsByName:        make(map[string]Job),
		disabledNames:     make(map[string]struct{}),
		metricsRecorder:   metricsRecorder,
		clock:             clock,
	}

	// Also set metrics on retry executor
	if metricsRecorder != nil {
		s.retryExecutor.SetMetricsRecorder(metricsRecorder)
	}

	return s
}

// maxConcurrentSkipWrapper returns a cron.JobWrapper that limits the total
// number of concurrent jobs across all entries. When the limit is reached,
// new invocations are skipped (not queued) and a log message is emitted.
//
// This is functionally equivalent to go-cron's cron.MaxConcurrentSkip but
// uses the scheduler's resizable concurrencySemaphore so that the limit
// can be adjusted via SetMaxConcurrentJobs before the scheduler starts.
func maxConcurrentSkipWrapper(logger cron.Logger, sem *concurrencySemaphore) cron.JobWrapper {
	return func(j cron.Job) cron.Job {
		return &maxConcurrentSkipJob{inner: j, sem: sem, logger: logger}
	}
}

// maxConcurrentSkipJob implements cron.Job and cron.JobWithContext.
// It acquires a slot from the shared concurrencySemaphore before running
// the inner job. If no slot is available, the invocation is skipped.
type maxConcurrentSkipJob struct {
	inner  cron.Job
	sem    *concurrencySemaphore
	logger cron.Logger
}

func (m *maxConcurrentSkipJob) Run() {
	m.RunWithContext(context.Background())
}

func (m *maxConcurrentSkipJob) RunWithContext(ctx context.Context) {
	ch := m.sem.getChan()
	select {
	case ch <- struct{}{}: // try to acquire slot
		defer func() { <-ch }()
		if jc, ok := m.inner.(cron.JobWithContext); ok {
			jc.RunWithContext(ctx)
		} else {
			m.inner.Run()
		}
	default:
		// cron.Logger only exposes Info and Error; use Info since skipping
		// is non-fatal.  The scheduler's own RunJob/Start paths log at Warn
		// via slog, but the middleware wrapper is limited to the cron interface.
		m.logger.Info("skip", "reason", "max concurrent reached",
			"limit", m.sem.getCap())
	}
}

// SetMaxConcurrentJobs configures the maximum number of concurrent jobs.
// The limit is enforced by the go-cron middleware chain (MaxConcurrentSkip
// pattern). When the limit is reached, new job invocations are skipped.
//
// This should be called before Start(); calling it on a running scheduler
// resizes the semaphore but in-flight jobs retain the previous channel.
func (s *Scheduler) SetMaxConcurrentJobs(maxJobs int) {
	if maxJobs < 1 {
		maxJobs = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron != nil && s.cron.IsRunning() {
		s.Logger.Warn("SetMaxConcurrentJobs called on running scheduler; in-flight jobs retain previous limit")
	}
	s.maxConcurrentJobs = maxJobs
	s.concurrencySem.resize(maxJobs)
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
// All jobs — including @triggered/@manual/@none — are registered with go-cron.
// Triggered schedules use go-cron's native TriggeredSchedule whose Next() returns
// zero time, so the scheduler never fires them automatically. They can be executed
// on demand via RunJob() which delegates to go-cron's TriggerEntryByName().
func (s *Scheduler) AddJobWithTags(j Job, tags ...string) error {
	if j.GetSchedule() == "" {
		return ErrEmptySchedule
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

	if IsTriggeredSchedule(j.GetSchedule()) {
		s.Logger.Info(fmt.Sprintf(
			"Triggered-only job registered %q - %q (will run only when triggered) - ID: %v",
			j.GetName(), j.GetCommand(), id,
		))
	} else {
		s.Logger.Info(fmt.Sprintf(
			"New job registered %q - %q - %q - ID: %v",
			j.GetName(), j.GetCommand(), j.GetSchedule(), id,
		))
	}
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
	delete(s.disabledNames, j.GetName())
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
				delete(s.disabledNames, job.GetName())
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

	// Build job name lookup map
	for _, j := range s.Jobs {
		s.jobsByName[j.GetName()] = j
	}

	// Wire dependency edges into go-cron using native DAG engine.
	// This must happen after all jobs are added to cron but before Start().
	if err := BuildWorkflowDependencies(s.cron, s.Jobs, s.Logger); err != nil {
		s.Logger.Error(fmt.Sprintf("Failed to build workflow dependencies: %v. "+
			"Jobs without dependencies will still run, but workflows may not execute as expected", err))
	}

	s.mu.Unlock()
	s.Logger.Debug("Starting scheduler")
	// All jobs — including triggered ones with ShouldRunOnStartup() — are registered
	// in go-cron with WithRunImmediately() when applicable. go-cron handles the
	// startup execution natively: it sets Next=now for runImmediately entries, which
	// causes them to fire once when the scheduler starts. For triggered schedules,
	// subsequent Next() calls return zero time so they remain dormant until explicitly
	// triggered again via TriggerEntryByName().
	s.cron.Start()

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

	s.wg.Wait()
	s.Logger.Debug("Scheduler stopped and all jobs completed")
}

// Entries returns all scheduled cron entries.
func (s *Scheduler) Entries() []cron.Entry {
	return s.cron.Entries()
}

// EntryByName returns a snapshot of the cron entry with the given name.
// Returns an invalid Entry (Entry.Valid() == false) if not found or if the
// scheduler's cron instance is nil.
func (s *Scheduler) EntryByName(name string) cron.Entry {
	if s.cron == nil {
		return cron.Entry{}
	}
	return s.cron.EntryByName(name)
}

// RunJob manually triggers a job by name. The job is executed through go-cron's
// TriggerEntryByName, which means it benefits from the full middleware chain
// (retry, timeout, etc.) and proper concurrency tracking.
// Returns ErrJobNotFound if the job does not exist or is disabled.
//
// Note: The context parameter is currently unused because go-cron's
// TriggerEntryByName does not accept a context. The job runs with its own
// internal context managed by go-cron. This means request-scoped cancellation
// is not supported for triggered executions.
func (s *Scheduler) RunJob(_ context.Context, jobName string) error {
	s.mu.RLock()
	_, exists := s.jobsByName[jobName]
	_, disabled := s.disabledNames[jobName]
	s.mu.RUnlock()

	if !exists || disabled {
		return fmt.Errorf("%w: %s", ErrJobNotFound, jobName)
	}

	// Delegate to go-cron's TriggerEntryByName for proper middleware chain execution.
	// This works for all job types including triggered schedules, since all jobs now
	// have cron entries (registered via TriggeredSchedule in PR #498). The
	// MaxConcurrentSkip middleware in the chain handles concurrency limiting.
	if err := s.cron.TriggerEntryByName(jobName); err != nil {
		return fmt.Errorf("trigger job %s: %w", jobName, err)
	}

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

// GetDisabledJobs returns a copy of all disabled/paused jobs.
func (s *Scheduler) GetDisabledJobs() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]Job, 0, len(s.disabledNames))
	for _, j := range s.Jobs {
		if _, ok := s.disabledNames[j.GetName()]; ok {
			jobs = append(jobs, j)
		}
	}
	return jobs
}

// GetAnyJob returns a job by name regardless of disabled state.
func (s *Scheduler) GetAnyJob(name string) Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.jobsByName != nil {
		return s.jobsByName[name]
	}
	j, _ := getJob(s.Jobs, name)
	return j
}

// GetActiveJobs returns a copy of all active (non-disabled) jobs.
func (s *Scheduler) GetActiveJobs() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]Job, 0, len(s.Jobs))
	for _, j := range s.Jobs {
		if _, disabled := s.disabledNames[j.GetName()]; !disabled {
			jobs = append(jobs, j)
		}
	}
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

// GetJob returns an active (non-disabled) job by name.
func (s *Scheduler) GetJob(name string) Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, disabled := s.disabledNames[name]; disabled {
		return nil
	}
	return s.lookupJob(name)
}

// GetDisabledJob returns a disabled/paused job by name.
func (s *Scheduler) GetDisabledJob(name string) Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, disabled := s.disabledNames[name]; !disabled {
		return nil
	}
	return s.lookupJob(name)
}

// lookupJob returns a job by name using the O(1) jobsByName map when available,
// falling back to linear scan for Scheduler instances created without NewScheduler.
func (s *Scheduler) lookupJob(name string) Job {
	if s.jobsByName != nil {
		return s.jobsByName[name]
	}
	j, _ := getJob(s.Jobs, name)
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
	_, disabled := s.disabledNames[name]
	if oldJob == nil || disabled {
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

// DisableJob pauses the job so it won't be scheduled or triggered, but keeps it
// for later enabling. Uses go-cron's native PauseEntryByName for all job types
// including triggered schedules (which now all have cron entries).
func (s *Scheduler) DisableJob(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, _ := getJob(s.Jobs, name)
	if j == nil {
		return fmt.Errorf("%w: %q", ErrJobNotFound, name)
	}
	if _, already := s.disabledNames[name]; already {
		return nil // already disabled
	}

	if err := s.cron.PauseEntryByName(name); err != nil {
		return fmt.Errorf("pause job: %w", err)
	}

	s.disabledNames[name] = struct{}{}
	s.Logger.Info(fmt.Sprintf("Job disabled %q", name))
	return nil
}

// EnableJob resumes a previously disabled/paused job. Uses go-cron's native
// ResumeEntryByName for all job types including triggered schedules.
func (s *Scheduler) EnableJob(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, disabled := s.disabledNames[name]; !disabled {
		return fmt.Errorf("%w: %q", ErrJobNotFound, name)
	}

	j, _ := getJob(s.Jobs, name)
	if j == nil {
		return fmt.Errorf("%w: %q", ErrJobNotFound, name)
	}

	if err := s.cron.ResumeEntryByName(name); err != nil {
		return fmt.Errorf("resume job: %w", err)
	}

	delete(s.disabledNames, name)
	s.Logger.Info(fmt.Sprintf("Job re-enabled %q", name))
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
	entryName := func(id cron.EntryID) string {
		if entry := cronInstance.Entry(id); entry.Name != "" {
			return entry.Name
		}
		return unknownJobName
	}

	// Aggregate results to determine overall workflow status
	status := workflowStatus(results)
	recorder.RecordWorkflowComplete(entryName(rootID), status)

	// Record individual job results
	for entryID, result := range results {
		recorder.RecordWorkflowJobResult(entryName(entryID), result.String())
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

	// NOTE: Concurrency limiting is handled by the go-cron middleware chain
	// (maxConcurrentSkipWrapper). Dependencies are handled by go-cron's native
	// DAG engine. No manual semaphore or workflow checks needed here.

	if !w.s.cron.IsRunning() {
		return
	}

	e, err := NewExecution()
	if err != nil {
		w.s.Logger.Error(fmt.Sprintf("failed to create execution: %v", err))
		return
	}

	// Ensure buffers are returned to pool when done
	defer e.Cleanup()

	jctx := NewContextWithContext(ctx, w.s, w.j, e)

	w.start(jctx)

	// Execute with retry logic
	err = w.s.retryExecutor.ExecuteWithRetry(w.j, jctx, func(c *Context) error {
		return c.Next()
	})

	w.stop(jctx, err)

	if w.s.onJobComplete != nil {
		success := err == nil && !jctx.Execution.Failed
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
