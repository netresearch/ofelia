// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/netresearch/go-cron"
)

const errFmtWrapQuoted = "%w: %q"

var (
	// ErrEmptyScheduler was returned by Start when it was called with no jobs
	// registered. Start now accepts an empty scheduler, so nothing returns
	// this error; it is retained for API compatibility.
	ErrEmptyScheduler = errors.New("unable to start an empty scheduler")
	// ErrEmptySchedule is returned by AddJob and AddJobWithTags when the job's
	// schedule string is empty. Jobs meant to run only on demand must use
	// @triggered, @manual or @none rather than an empty schedule.
	ErrEmptySchedule = errors.New("unable to add a job with an empty schedule")
)

// IsTriggeredSchedule returns true if the schedule string indicates the job
// should only run when triggered (not on a time-based schedule). This is a
// convenience wrapper around string comparison for the three recognized keywords.
// See schedule_keywords.go for the full list of recognized schedule strings.
func IsTriggeredSchedule(schedule string) bool {
	return schedule == TriggeredSchedule || schedule == ManualSchedule || schedule == NoneSchedule
}

// Scheduler owns the set of registered jobs and drives them through go-cron.
// It handles registration and removal, name-based lookup, enable/disable,
// manual triggering, the global middleware chain propagated to every job,
// retries, and a concurrency limit shared across all entries.
//
// Construct it with NewScheduler or one of its variants; a zero value has no
// cron instance and cannot run jobs. Methods are safe for concurrent use, and
// the exported Jobs and Removed slices are mutated under the scheduler's own
// lock — copy them (GetActiveJobs, GetRemovedJobs) rather than ranging over
// them from another goroutine.
type Scheduler struct {
	Jobs    []Job
	Removed []Job
	Logger  *slog.Logger

	middlewareContainer
	cron              *cron.Cron
	mu                sync.RWMutex
	maxConcurrentJobs int
	concurrencySem    *concurrencySemaphore // go-cron middleware semaphore
	retryExecutor     *RetryExecutor
	jobsByName        map[string]Job
	disabledNames     map[string]struct{}
	// unschedulable records jobs the scheduler refused, keyed by name with the
	// reason as the value. A refused job never runs, and the only earlier trace
	// was a log line; keeping it here lets the health report say so, and covers
	// jobs added long after startup from Docker labels as well as those from
	// the config.
	unschedulable   map[string]string
	metricsRecorder MetricsRecorder
	clock           Clock
	onJobComplete   func(jobName string, success bool)

	// gate keeps a replacement's runs from starting beside the stopping run
	// of the job it replaced; see runGate. regMu makes each change of a
	// go-cron entry and the matching gate change one step, so a concurrent
	// update and removal of one name cannot leave the gate describing an
	// entry go-cron no longer has. It is held only around those calls.
	gate  runGate
	regMu sync.Mutex

	// orphanPollInterval overrides defaultOrphanPollInterval when positive;
	// tests set it before Start.
	orphanPollInterval time.Duration

	// globalMaxRuntime is `[global] max-runtime`, the bound of every run of a
	// job that inherits it (see MaxRuntimeProvider). It is read while the run
	// is in progress, not copied into the job, so a changed value needs no
	// re-registration. maxRuntimeChanged is closed and replaced on every
	// change, which wakes the deadline watchers of runs in progress.
	maxRuntimeMu      sync.Mutex
	globalMaxRuntime  time.Duration
	maxRuntimeChanged chan struct{}

	// Durable schedule state (ADR-005, schedule_state.go). durableMu guards
	// records, durable and the watcher context. Lock order: durableMu may be
	// held while taking mu, never the other way round.
	durableMu sync.Mutex
	records   ScheduleRecordStore
	durable   map[string]*durableEntry
	// watchersDone is closed by Stop and ends the orphan watchers; a later
	// Start replaces it.
	watchersDone chan struct{}
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

// resize replaces the semaphore channel with a new one of capacity n.
//
// Concurrency note: the write lock prevents concurrent getChan calls from
// observing the channel swap, but goroutines that already obtained the old
// channel reference will continue using it until they release their slot.
// During the transition window, up to old_cap + new_cap goroutines could
// theoretically run concurrently. This is acceptable because resize is
// intended to be called before Start() (see SetMaxConcurrentJobs doc).
// Calling it on a running scheduler logs a warning and is best-effort.
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

// NewScheduler returns a Scheduler with default settings: no metrics recorder,
// go-cron's default minimum @every interval (1s), the real clock, and a limit
// of 10 concurrent jobs. It does not start the cron loop — call Start for that.
// Use NewSchedulerWithMetrics, NewSchedulerWithOptions or NewSchedulerWithClock
// to change those defaults.
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

	clock := GetDefaultClock()
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
	s.watchersDone = make(chan struct{})

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

// Run implements cron.Job. Defensive fallback only: maxConcurrentSkipJob
// also implements cron.JobWithContext, so go-cron always invokes
// RunWithContext on the happy path. Use context.TODO() to flag the
// fallback intent. The per-job deadline is applied downstream by
// jobWrapper.runWithCtx via boundJobContext (issue #638).
func (m *maxConcurrentSkipJob) Run() {
	m.RunWithContext(context.TODO())
}

// RunWithContext attempts to acquire a slot from the shared concurrencySemaphore
// before delegating execution to the wrapped job. If no slot is immediately
// available, the invocation is skipped and logged via cron.Logger.
func (m *maxConcurrentSkipJob) RunWithContext(ctx context.Context) {
	// A job with durable schedule state runs its per-entry check first and
	// then waits for a slot instead of being dropped here (ADR-005).
	if w, ok := m.inner.(*jobWrapper); ok && w.durable() {
		w.RunWithContext(ctx)
		return
	}
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
		// is non-fatal.  Via CronUtils, cron.Logger.Info maps to slog.Debug,
		// so cron-scheduled skips appear at Debug level while the scheduler's
		// own RunJob/Start paths log at Warn via slog directly.  This is
		// intentional: frequent cron skips stay quiet, manual skips are visible.
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

// SetMetricsRecorder installs the recorder used for retry metrics and stores it
// on the scheduler. It can be called at any time, including while running.
//
// It does not retrofit the go-cron observability hooks (job start, completion,
// scheduling and workflow results): those are wired only when a recorder is
// passed at construction time, so pass one to NewSchedulerWithMetrics or
// NewSchedulerWithOptions if you want them.
func (s *Scheduler) SetMetricsRecorder(recorder MetricsRecorder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metricsRecorder = recorder
	if s.retryExecutor != nil {
		s.retryExecutor.SetMetricsRecorder(recorder)
	}
}

// SetClock replaces the clock stored on the scheduler. It exists for tests and
// does not change the clock go-cron schedules with, which is fixed at
// construction — use NewSchedulerWithClock to drive scheduling from a fake
// clock.
func (s *Scheduler) SetClock(c Clock) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock = c
}

// SetOnJobComplete registers a callback invoked after every job run with the
// job's name and whether it succeeded (no error and the execution not marked
// failed). Passing nil clears it. Only one callback is held; a second call
// replaces the first.
//
// The callback runs synchronously on the job's own goroutine at the end of the
// run, so it must not block. It may be changed while the scheduler is running:
// each run snapshots it under the lock.
func (s *Scheduler) SetOnJobComplete(callback func(jobName string, success bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onJobComplete = callback
}

// AddJob registers j under its configured name, equivalent to AddJobWithTags
// with no tags. See AddJobWithTags for the registration semantics and the
// errors returned.
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
	return s.addJob(j, false, tags...)
}

// AddJobDisabled registers j already disabled: its cron entry is added paused,
// so go-cron never dispatches it, not even for run-on-startup. Adding the job
// and then calling DisableJob would leave a window in which it can start.
func (s *Scheduler) AddJobDisabled(j Job) error {
	return s.addJob(j, true)
}

func (s *Scheduler) addJob(j Job, disabled bool, tags ...string) error {
	if j.GetSchedule() == "" {
		s.recordUnschedulable(j.GetName(), ErrEmptySchedule)
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
	if disabled {
		opts = append(opts, cron.WithPaused())
	}

	// Apply global middlewares BEFORE adding to cron, because WithRunImmediately()
	// may cause the job to execute immediately after AddJob returns — before we'd
	// get a chance to apply middlewares afterwards.
	j.Use(s.Middlewares()...)

	// A job with durable schedule state is evaluated against its record
	// before its entry goes live; Start does it for jobs added before it.
	var launch *pendingLaunch
	evaluated := false
	if policy, ok := s.durablePolicy(j); ok && s.cron.IsRunning() {
		launch = s.prepareDurableEntry(j, policy, disabled)
		evaluated = true
	}

	w := &jobWrapper{s: s, j: j}
	s.regMu.Lock()
	id, err := s.cron.AddJob(j.GetSchedule(), w, opts...)
	if err == nil {
		s.gate.register(w)
	}
	s.regMu.Unlock()
	if err != nil {
		s.Logger.Warn(fmt.Sprintf(
			"Failed to register job %q - %q - %q",
			j.GetName(), j.GetCommand(), j.GetSchedule(),
		))
		s.recordUnschedulable(j.GetName(), err)
		return fmt.Errorf("add cron job: %w", err)
	}
	j.SetCronJobID(uint64(id))
	s.mu.Lock()
	s.Jobs = append(s.Jobs, j)
	s.jobsByName[j.GetName()] = j
	if disabled {
		s.disabledNames[j.GetName()] = struct{}{}
	}
	// A name that registers now is no longer unschedulable: a corrected config
	// reloaded at runtime has to clear the old complaint, or the health report
	// would stay degraded until a restart.
	delete(s.unschedulable, j.GetName())
	s.mu.Unlock()
	if launch != nil {
		s.launch(launch)
	} else if evaluated {
		s.resumeAfterRegistration(j.GetName())
	}

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

// RemoveJob deregisters j so it will not fire again, cancels a run in
// progress, drops the job from the active jobs and appends it to Removed. It
// does not wait for the canceled run to stop: until it has, IsRemovedJobRunning
// reports true for j, and a job registered under the same name skips its
// fires (see runGate). Removal is by name, so a job that was never registered
// is silently ignored. The error return is always nil and exists for API
// stability.
func (s *Scheduler) RemoveJob(j Job) error {
	s.Logger.Info(fmt.Sprintf(
		"Job deregistered (will not fire again) %q - %q - %q - ID: %v",
		j.GetName(), j.GetCommand(), j.GetSchedule(), j.GetCronJobID(),
	))
	// Retire before removing: a fire go-cron dispatched before the removal
	// but that has not reached the gate yet is then refused instead of
	// starting after the removal returned.
	s.regMu.Lock()
	s.gate.retire(j.GetName())
	s.cron.RemoveByName(j.GetName())
	s.regMu.Unlock()
	s.mu.Lock()
	for i, job := range s.Jobs {
		if job == j || job.GetCronJobID() == j.GetCronJobID() {
			s.Jobs = append(s.Jobs[:i], s.Jobs[i+1:]...)
			break
		}
	}
	delete(s.jobsByName, j.GetName())
	// A job removed from the config is no longer expected to run, so a past
	// refusal stops being a complaint about the current state.
	delete(s.unschedulable, j.GetName())
	delete(s.disabledNames, j.GetName())
	s.Removed = append(s.Removed, j)
	s.mu.Unlock()
	return nil
}

// RemoveJobsByTag removes all jobs with the specified tag.
// Returns the number of jobs removed.
func (s *Scheduler) RemoveJobsByTag(tag string) int {
	// Get entries by tag before removal for logging
	s.regMu.Lock()
	entries := s.cron.EntriesByTag(tag)
	if len(entries) == 0 {
		s.regMu.Unlock()
		return 0
	}
	for _, entry := range entries {
		s.gate.retire(entry.Name)
	}

	// Remove from cron using O(1) tag removal
	count := s.cron.RemoveByTag(tag)
	s.regMu.Unlock()

	// Update our internal state
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range entries {
		// Find and remove from Jobs slice. slices.Backward captures the
		// slice header once, so the break right after the removal is what
		// keeps this safe — no iteration step runs over the stale header.
		for i, job := range slices.Backward(s.Jobs) {
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

// Start wires the job dependency graph into go-cron and starts the cron loop.
// It does not block — the loop runs on its own goroutine — and always returns
// nil, including for a scheduler with no jobs registered. A failure to build
// the dependency graph is logged rather than returned, so that one misconfigured
// dependency cannot take down scheduling for every other job.
//
// Jobs configured to run on startup fire once here, dispatched by go-cron.
// Starting an already-running scheduler is handled by go-cron and does not
// double-start the loop.
func (s *Scheduler) Start() error {
	s.mu.Lock()

	// Build job name lookup map
	for _, j := range s.Jobs {
		s.jobsByName[j.GetName()] = j
	}

	// Wire dependency edges into go-cron using native DAG engine.
	// This must happen after all jobs are added to cron but before Start().
	//
	// BuildWorkflowDependencies errors are non-fatal: jobs without dependencies
	// continue to run on their cron schedule even if DAG wiring fails.
	// This prevents a misconfigured dependency from taking down all scheduling.
	if err := BuildWorkflowDependencies(s.cron, s.Jobs, s.Logger); err != nil {
		s.Logger.Error(fmt.Sprintf("Failed to build workflow dependencies: %v. "+
			"Jobs without dependencies will still run, but workflows may not execute as expected", err))
	}

	jobs := slices.Clone(s.Jobs)
	s.mu.Unlock()
	s.renewWatchers()

	// Jobs with durable schedule state are evaluated against their records
	// before go-cron starts; the occurrences that result start afterwards.
	var launches []*pendingLaunch
	for _, j := range jobs {
		if policy, ok := s.durablePolicy(j); ok {
			if l := s.prepareDurableEntry(j, policy, s.IsJobDisabled(j.GetName())); l != nil {
				launches = append(launches, l)
			}
		}
	}

	s.Logger.Debug("Starting scheduler")
	// All jobs — including triggered ones with ShouldRunOnStartup() — are registered
	// in go-cron with WithRunImmediately() when applicable. go-cron handles the
	// startup execution natively: it sets Next=now for runImmediately entries, which
	// causes them to fire once when the scheduler starts. For triggered schedules,
	// subsequent Next() calls return zero time so they remain dormant until explicitly
	// triggered again via TriggerEntryByName().
	s.cron.Start()
	for _, l := range launches {
		s.launch(l)
	}

	return nil
}

// DefaultStopTimeout is the default timeout for graceful shutdown.
const DefaultStopTimeout = 30 * time.Second

// Stop stops the cron loop and blocks for up to DefaultStopTimeout (30s) while
// running jobs finish. It returns an error wrapping ErrSchedulerTimeout if they
// have not finished by then — the jobs are not killed and may still be running
// when it returns. Use StopWithTimeout for a different bound, or StopAndWait to
// wait indefinitely.
func (s *Scheduler) Stop() error {
	return s.StopWithTimeout(DefaultStopTimeout)
}

// StopWithTimeout stops the scheduler with a graceful shutdown timeout.
// It stops accepting new jobs, then waits up to the timeout for running jobs to complete.
// Returns nil if all jobs completed, or an error if the timeout was exceeded.
func (s *Scheduler) StopWithTimeout(timeout time.Duration) error {
	s.endOrphanWatchers()
	// Use go-cron's StopWithTimeout for graceful shutdown
	completed := s.cron.StopWithTimeout(timeout)

	if !completed {
		s.Logger.Warn(fmt.Sprintf("Scheduler stop timed out after %v - some jobs may still be running", timeout))
		return fmt.Errorf("%w after %v", ErrSchedulerTimeout, timeout)
	}
	s.Logger.Debug("Scheduler stopped gracefully")
	return nil
}

// StopAndWait stops the scheduler and waits indefinitely for all jobs to complete.
func (s *Scheduler) StopAndWait() {
	s.endOrphanWatchers()
	s.cron.StopAndWait()
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
	if j := s.GetAnyJob(jobName); j != nil {
		_, durable := s.durablePolicy(j)
		if durable && s.durableBusy(jobName) {
			return fmt.Errorf("%w: %s", ErrJobBusy, jobName)
		}
		// The run would be skipped by the name lock (runGate) while a
		// removed or replaced job's canceled run stops.
		if s.gate.blocked(jobName, durable) {
			return fmt.Errorf("%w: %s", ErrJobBusy, jobName)
		}
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

// fireDeferredStartup fires the run-on-startup run of w that the name lock
// (runGate) deferred while a removed or replaced job's run stopped.
func (s *Scheduler) fireDeferredStartup(w *jobWrapper) {
	name := w.j.GetName()
	info := cron.RunInfo{ScheduledTime: s.now(), Trigger: cron.TriggerImmediate}
	if err := s.cron.TriggerEntryWithInfoByName(name, info); err != nil {
		s.Logger.Warn("could not start the deferred run-on-startup run", "job", name, "error", err)
	}
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

// IsJobDisabled reports whether the named job is registered and disabled.
func (s *Scheduler) IsJobDisabled(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, disabled := s.disabledNames[name]
	return disabled
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

// GetJobSnapshot returns the active, disabled and removed job lists as they
// stood at one instant.
//
// Calling GetActiveJobs, GetDisabledJobs and GetRemovedJobs in sequence takes
// three separate read locks, so a job disabled or removed between two of them
// appears in both lists or in neither. The dashboard renders the lists without
// deduplicating by name, so such a job showed as two rows until the next poll
// healed it.
func (s *Scheduler) GetJobSnapshot() (active, disabled, removed []Job) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	active = make([]Job, 0, len(s.Jobs))
	disabled = make([]Job, 0, len(s.disabledNames))
	for _, j := range s.Jobs {
		if _, off := s.disabledNames[j.GetName()]; off {
			disabled = append(disabled, j)
		} else {
			active = append(active, j)
		}
	}
	removed = make([]Job, len(s.Removed))
	copy(removed, s.Removed)
	return active, disabled, removed
}

// recordUnschedulable notes that a job was refused, so the health report can
// say a configured job is not running rather than leaving it to whoever reads
// the startup log.
func (s *Scheduler) recordUnschedulable(name string, reason error) {
	if name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unschedulable == nil {
		s.unschedulable = make(map[string]string)
	}
	s.unschedulable[name] = reason.Error()
}

// GetUnschedulableJobs returns a copy of the jobs the scheduler refused,
// keyed by job name with the reason as the value. Empty means every job that
// was offered is registered.
func (s *Scheduler) GetUnschedulableJobs() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.unschedulable))
	maps.Copy(out, s.unschedulable)
	return out
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
// existing named entry using go-cron's UpdateEntryJobByName. Replacing the job
// cancels the old job's run in progress, and UpdateJob does not wait for it to
// stop: until it has, the new job skips its fires (see runGate).
//
// Disabled jobs are updated in place and stay disabled: refusing them would
// force callers into remove+add, which resumes the job and files the old copy
// under Removed.
//
// Returns ErrJobNotFound if no job with the given name exists.
func (s *Scheduler) UpdateJob(name string, newSchedule string, newJob Job) error {
	s.mu.RLock()
	oldJob, _ := getJob(s.Jobs, name)
	s.mu.RUnlock()
	if oldJob == nil {
		return fmt.Errorf(errFmtWrapQuoted, ErrJobNotFound, name)
	}

	newJob.Use(s.Middlewares()...)

	// Replacing the entry's job cancels the old job's run like a removal
	// does, so the old registration is retired first; see RemoveJob. A
	// failed update restores it, under the same regMu, so a removal of the
	// name in between cannot be undone by the restore.
	s.regMu.Lock()
	old := s.gate.retire(name)
	w := &jobWrapper{s: s, j: newJob}
	if err := s.cron.UpdateEntryJobByName(name, newSchedule, w); err != nil {
		s.gate.restore(old)
		s.regMu.Unlock()
		return fmt.Errorf("update job: %w", err)
	}
	s.gate.register(w)
	s.regMu.Unlock()

	if err := s.applyUpdatedJob(name, newJob); err != nil {
		return err
	}

	s.Logger.Info(fmt.Sprintf("Job updated %q - %q", name, newSchedule))
	return nil
}

// applyUpdatedJob records newJob as the live job for name. It runs after
// the cron entry has already been replaced, so it must tolerate the job
// having vanished in between: RemoveJob drops the cron entry BEFORE it
// takes s.mu, so a delete that began after UpdateJob's pre-check can
// complete entirely inside that window.
//
// Two orderings matter here, and both used to be wrong:
//
// The existence re-check comes first. Writing s.jobsByName
// unconditionally resurrected a deleted name as a ghost — present in the
// map, absent from s.Jobs (the replace loop finds nothing) and holding no
// cron entry, so it never fired again while the API kept reporting it.
//
// The re-pause comes before the maps change, because it is the last step
// that can fail. go-cron v0.16.0's updateSchedule mutates the entry in
// place and never clears Paused, so the call is a no-op today and exists
// to keep the invariant should that contract change — but its one
// reachable error, ErrEntryNotFound, used to arrive after s.Jobs and
// s.jobsByName had been rewritten, leaving the caller a 500 for an update
// that had already taken effect. Failing here leaves the scheduler
// exactly as it was.
//
// Lock safety while calling PauseEntryByName: see DisableJob's doc comment.
func (s *Scheduler) applyUpdatedJob(name string, newJob Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, live := s.jobsByName[name]; !live {
		return fmt.Errorf(errFmtWrapQuoted, ErrJobNotFound, name)
	}

	if _, disabled := s.disabledNames[name]; disabled {
		if err := s.cron.PauseEntryByName(name); err != nil {
			return fmt.Errorf("re-pause updated job: %w", err)
		}
	}

	for i, j := range s.Jobs {
		if j.GetName() == name {
			s.Jobs[i] = newJob
			break
		}
	}
	s.jobsByName[name] = newJob
	return nil
}

// DisableJob pauses the job so it won't be scheduled or triggered, but keeps it
// for later enabling. Uses go-cron's native PauseEntryByName for all job types
// including triggered schedules (which now all have cron entries).
//
// Holding s.mu while calling go-cron's PauseEntryByName is safe from deadlock:
// go-cron's setPausedState acquires c.runningMu and sends a request to the run
// loop, which sets a boolean flag without calling back into ofelia's Scheduler.
// Job execution happens in separate goroutines that do not hold c.runningMu,
// and runWithCtx only acquires s.mu.RLock (compatible with the Lock held here).
func (s *Scheduler) DisableJob(name string) error {
	j, changed, err := s.disableJob(name)
	if err != nil || !changed {
		return err
	}
	if _, ok := s.durablePolicy(j); ok {
		s.durableDisabled(name)
	}
	return nil
}

// disableJob pauses the job under s.mu. changed is false when it was
// disabled already.
func (s *Scheduler) disableJob(name string) (j Job, changed bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, _ = getJob(s.Jobs, name)
	if j == nil {
		return nil, false, fmt.Errorf(errFmtWrapQuoted, ErrJobNotFound, name)
	}
	if _, already := s.disabledNames[name]; already {
		return j, false, nil
	}

	if err := s.cron.PauseEntryByName(name); err != nil {
		return nil, false, fmt.Errorf("pause job: %w", err)
	}

	s.disabledNames[name] = struct{}{}
	s.Logger.Info(fmt.Sprintf("Job disabled %q", name))
	return j, true, nil
}

// EnableJob resumes a previously disabled/paused job. Uses go-cron's native
// ResumeEntryByName for all job types including triggered schedules.
//
// Lock safety: same as DisableJob -- see its doc comment for rationale.
func (s *Scheduler) EnableJob(name string) error {
	j, changed, err := s.enableJob(name)
	if err != nil || !changed {
		return err
	}
	if _, ok := s.durablePolicy(j); ok {
		s.durableEnabled(name)
	}
	return nil
}

// enableJob resumes the job under s.mu. changed is false when it was enabled
// already.
func (s *Scheduler) enableJob(name string) (j Job, changed bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, disabled := s.disabledNames[name]; !disabled {
		// Job is not in the disabled list. Check if it's an active job
		// (already enabled) — if so, this is an idempotent no-op.
		if active, ok := s.jobsByName[name]; ok {
			return active, false, nil
		}
		return nil, false, fmt.Errorf(errFmtWrapQuoted, ErrJobNotFound, name)
	}

	j, _ = getJob(s.Jobs, name)
	if j == nil {
		return nil, false, fmt.Errorf(errFmtWrapQuoted, ErrJobNotFound, name)
	}

	if err := s.cron.ResumeEntryByName(name); err != nil {
		return nil, false, fmt.Errorf("resume job: %w", err)
	}

	delete(s.disabledNames, name)
	s.Logger.Info(fmt.Sprintf("Job re-enabled %q", name))
	return j, true, nil
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
	if s.cron == nil {
		return false
	}
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

// IsRemovedJobRunning reports whether j was removed or replaced and its
// canceled run is still stopping. IsJobRunning cannot answer that: it asks by
// name, which after a removal finds no entry, or the replacement's.
func (s *Scheduler) IsRemovedJobRunning(j Job) bool {
	return s.gate.retiredRunning(j)
}

// defaultJobMaxRuntime is the upper bound applied to job execution
// contexts when neither the job nor the global config exposes a
// MaxRuntime. It mirrors the documented default for
// `[global] max-runtime` (24h). See issue #638.
const defaultJobMaxRuntime = 24 * time.Hour

// MaxRuntimeProvider is implemented by job types that expose a per-job
// maximum runtime. Currently RunJob, RunServiceJob and ComposeJob satisfy
// this interface; ExecJob and LocalJob do not, and run under
// defaultJobMaxRuntime.
type MaxRuntimeProvider interface {
	// GetMaxRuntime returns the job's own max-runtime; 0 means none.
	GetMaxRuntime() time.Duration
	// InheritsGlobalMaxRuntime reports whether a job without a max-runtime
	// of its own runs under the scheduler's `[global] max-runtime`
	// (SetGlobalMaxRuntime). A compose job in exec mode does not.
	InheritsGlobalMaxRuntime() bool
}

// SetGlobalMaxRuntime sets `[global] max-runtime`, the bound of every job
// that inherits it. The value applies to the next run and to runs in
// progress: a run whose elapsed time already reaches the new value ends with
// ErrMaxTimeRunning. A value <= 0 means defaultJobMaxRuntime.
func (s *Scheduler) SetGlobalMaxRuntime(d time.Duration) {
	s.maxRuntimeMu.Lock()
	defer s.maxRuntimeMu.Unlock()
	if d == s.globalMaxRuntime {
		return
	}
	s.globalMaxRuntime = d
	if s.maxRuntimeChanged != nil {
		close(s.maxRuntimeChanged)
	}
	s.maxRuntimeChanged = make(chan struct{})
}

// GlobalMaxRuntime returns the bound an inheriting job runs under now, and a
// channel that is closed when the value next changes.
func (s *Scheduler) GlobalMaxRuntime() (time.Duration, <-chan struct{}) {
	s.maxRuntimeMu.Lock()
	defer s.maxRuntimeMu.Unlock()
	if s.maxRuntimeChanged == nil {
		s.maxRuntimeChanged = make(chan struct{})
	}
	d := s.globalMaxRuntime
	if d <= 0 {
		d = defaultJobMaxRuntime
	}
	return d, s.maxRuntimeChanged
}

// MaxRuntimeFor returns the bound a run of j starting now runs under: the
// job's own max-runtime, else the current `[global] max-runtime` for a job
// that inherits it, else defaultJobMaxRuntime.
func (s *Scheduler) MaxRuntimeFor(j Job) time.Duration {
	if mp, ok := j.(MaxRuntimeProvider); ok {
		if d := mp.GetMaxRuntime(); d > 0 {
			return d
		}
		if mp.InheritsGlobalMaxRuntime() {
			d, _ := s.GlobalMaxRuntime()
			return d
		}
	}
	return defaultJobMaxRuntime
}

// runContext bounds one run of j. A job with a max-runtime of its own, and a
// job that does not inherit the global value, gets a fixed deadline from
// boundJobContext. An inheriting job gets a deadline that follows
// SetGlobalMaxRuntime while the run is in progress.
func (s *Scheduler) runContext(parent context.Context, j Job) (context.Context, context.CancelFunc) {
	if mp, ok := j.(MaxRuntimeProvider); ok && mp.GetMaxRuntime() <= 0 && mp.InheritsGlobalMaxRuntime() {
		return s.inheritedDeadlineContext(parent)
	}
	return boundJobContext(parent, j, defaultJobMaxRuntime)
}

// inheritedDeadlineContext returns a child of parent that is canceled with
// the cause ErrMaxTimeRunning once the time since the call reaches the
// current global max-runtime. A change of the global value re-arms the
// deadline against the same start time. The caller must invoke the returned
// cancel when the run finishes, which also ends the watcher goroutine.
func (s *Scheduler) inheritedDeadlineContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	start := time.Now()
	go func() {
		for {
			d, changed := s.GlobalMaxRuntime()
			remaining := d - time.Since(start)
			if remaining <= 0 {
				cancel(ErrMaxTimeRunning)
				return
			}
			timer := time.NewTimer(remaining)
			select {
			case <-timer.C:
				// Loop: the value can have changed just before the timer fired.
			case <-changed:
				timer.Stop()
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}
	}()
	return ctx, func() { cancel(context.Canceled) }
}

// boundJobContext returns a child of parent with a deadline derived from
// the job's MaxRuntime when the job implements MaxRuntimeProvider, or
// from defaultMax otherwise. The caller must invoke the returned cancel
// when the job finishes to release the timer.
//
// This is the scheduler-side enforcement complement to RunJob.startAndWait's
// inner WithTimeout: it ensures every job (including ExecJob, RunServiceJob,
// LocalJob, ComposeJob) runs under a bounded context, so a wedged Docker
// upstream cannot stall a goroutine indefinitely. See issue #638.
func boundJobContext(parent context.Context, j Job, defaultMax time.Duration) (context.Context, context.CancelFunc) {
	d := defaultMax
	if mp, ok := j.(MaxRuntimeProvider); ok {
		if jm := mp.GetMaxRuntime(); jm > 0 {
			d = jm
		}
	}
	if d <= 0 {
		// Defensive: never return parent unwrapped with a no-op cancel
		// confused for "deadline applied".
		d = defaultJobMaxRuntime
	}
	return context.WithTimeout(parent, d)
}

type jobWrapper struct {
	s *Scheduler
	j Job
	// retired is set when the job is removed or replaced; guarded by
	// s.gate.mu.
	retired bool
	// keepsRecord is set when a run of this registration went through the
	// per-entry check of ADR-005, which counts it as busy; guarded by
	// s.gate.mu.
	keepsRecord bool
}

// Compile-time assertion: jobWrapper implements cron.JobWithContext.
var _ cron.JobWithContext = (*jobWrapper)(nil)

// durable reports whether the wrapped job keeps durable schedule state.
func (w *jobWrapper) durable() bool {
	_, ok := w.s.durablePolicy(w.j)
	return ok
}

// Run implements cron.Job. Called by go-cron for jobs that don't support
// context. Defensive only: jobWrapper also implements
// cron.JobWithContext (see assertion above), so go-cron always uses
// RunWithContext on the happy path. Use context.TODO() to flag the
// fallback intent and let runWithCtx apply the per-job deadline. See
// issue #638.
func (w *jobWrapper) Run() {
	w.runWithCtx(context.TODO())
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

	durable := w.durable()
	switch w.s.gate.enter(w, durable) {
	case gateRetired:
		w.s.Logger.Debug("skip: the job was removed or replaced", "job", w.j.GetName())
		return
	case gateDraining:
		if info, ok := cron.RunInfoFromContext(ctx); ok && info.Trigger == cron.TriggerImmediate {
			w.s.gate.deferStartup(w, durable)
			w.s.Logger.Warn("defer: the run-on-startup run waits until the canceled run of the removed or replaced job has stopped",
				"job", w.j.GetName())
			return
		}
		w.s.Logger.Warn("skip: the canceled run of the removed or replaced job is still stopping",
			"job", w.j.GetName())
		return
	case gateRun:
	}
	defer func() {
		if due := w.s.gate.leave(w); due != nil {
			w.s.fireDeferredStartup(due)
		}
	}()

	// NOTE: Concurrency limiting is handled by the go-cron middleware chain
	// (maxConcurrentSkipWrapper). Dependencies are handled by go-cron's native
	// DAG engine. No manual semaphore or workflow checks needed here.

	if !w.s.cron.IsRunning() {
		return
	}

	// Snapshot the callback under the read lock so that a concurrent
	// SetOnJobComplete call does not race with the nil check + invocation
	// below. SetOnJobComplete writes under s.mu, so reading under RLock
	// establishes a happens-before relationship.
	w.s.mu.RLock()
	onComplete := w.s.onJobComplete
	w.s.mu.RUnlock()

	e, err := NewExecution()
	if err != nil {
		w.s.Logger.Error(fmt.Sprintf("failed to create execution: %v", err))
		return
	}

	// Ensure buffers are returned to pool when done. stop does it before it
	// publishes the run; this covers a run that ends without reaching stop.
	defer e.Cleanup()

	// A job with durable schedule state passes the per-entry check of
	// ADR-005 and takes a global slot before it runs. Its end is recorded in
	// a defer, so a run that panics still releases the slot and the entry;
	// it then counts as failed.
	outcome := outcomeFailed
	durableRun := false
	if policy, ok := w.s.durablePolicy(w.j); ok {
		adm := w.s.admit(ctx, w.j, policy, e.ID)
		if adm == nil {
			return
		}
		durableRun = true
		defer func() { w.s.finishDispatch(adm, outcome) }()
	}

	// Bound the per-run context with the job's MaxRuntime (or the
	// global or default bound) so a wedged Docker upstream cannot stall
	// this goroutine indefinitely. Applied at the wrapper layer so all job
	// types (ExecJob, RunJob, RunServiceJob, LocalJob, ComposeJob)
	// inherit the bound, not just RunJob/RunServiceJob which already
	// re-wrap inside startAndWait. See issue #638.
	boundedCtx, cancelBound := w.s.runContext(ctx, w.j)
	defer cancelBound()

	jctx := NewContextWithContext(boundedCtx, w.s, w.j, e)

	w.start(jctx)

	// Execute with retry logic
	err = w.s.retryExecutor.ExecuteWithRetry(w.j, jctx, func(c *Context) error {
		return c.Next()
	})

	w.stop(jctx, err)

	if durableRun {
		outcome = runOutcome(ctx, err, jctx.Execution)
	}

	if onComplete != nil {
		success := err == nil && !jctx.Execution.Failed
		onComplete(w.j.GetName(), success)
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
	defer w.publish(ctx)

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

// publish makes the ended run the job's last run. It first moves the output
// out of the pooled buffers (Execution.Cleanup), so readers of the last run,
// such as the web API on another goroutine, only ever see strings that no
// longer change. Published with the buffers still attached, a reader could
// race the Cleanup that follows and read a buffer the pool had already
// handed to another run (#898).
func (w *jobWrapper) publish(ctx *Context) {
	ctx.Execution.Cleanup()
	if l, ok := ctx.Job.(interface{ SetLastRun(*Execution) }); ok {
		l.SetLastRun(ctx.Execution)
	}
}
