package core

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

var (
	ErrEmptyScheduler = errors.New("unable to start an empty scheduler")
	ErrEmptySchedule  = errors.New("unable to add a job with an empty schedule")
)

type Scheduler struct {
	Jobs     []Job
	Removed  []Job
	Disabled []Job
	Logger   Logger

	middlewareContainer
	cron                 *cron.Cron
	wg                   sync.WaitGroup
	isRunning            bool
	mu                   sync.RWMutex // Protect isRunning and wg/removed operations
	maxConcurrentJobs    int
	jobSemaphore         chan struct{} // Limits concurrent job execution
	retryExecutor        *RetryExecutor
	workflowOrchestrator *WorkflowOrchestrator
	jobsByName           map[string]Job  // Quick lookup for jobs by name
	metricsRecorder      MetricsRecorder // Metrics recorder for job metrics
	cleanupTicker        *time.Ticker    // Ticker for workflow cleanup
	cleanupStop          chan struct{}   // Signal to stop cleanup
}

func NewScheduler(l Logger) *Scheduler {
	cronUtils := NewCronUtils(l)
	cron := cron.New(
		cron.WithParser(
			cron.NewParser(
				cron.SecondOptional|cron.Minute|cron.Hour|
					cron.Dom|cron.Month|cron.Dow|cron.Descriptor,
			),
		),
		cron.WithLogger(cronUtils),
		cron.WithChain(cron.Recover(cronUtils)),
	)

	// Default to 10 concurrent jobs, can be configured
	maxConcurrent := 10

	s := &Scheduler{
		Logger:            l,
		cron:              cron,
		maxConcurrentJobs: maxConcurrent,
		jobSemaphore:      make(chan struct{}, maxConcurrent),
		retryExecutor:     NewRetryExecutor(l),
		jobsByName:        make(map[string]Job),
	}

	// Initialize workflow orchestrator
	s.workflowOrchestrator = NewWorkflowOrchestrator(s, l)

	// Initialize cleanup channels
	s.cleanupStop = make(chan struct{})

	return s
}

// SetMaxConcurrentJobs configures the maximum number of concurrent jobs
func (s *Scheduler) SetMaxConcurrentJobs(max int) {
	if max < 1 {
		max = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxConcurrentJobs = max
	s.jobSemaphore = make(chan struct{}, max)
}

// SetMetricsRecorder sets the metrics recorder for the scheduler
func (s *Scheduler) SetMetricsRecorder(recorder MetricsRecorder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metricsRecorder = recorder
	// Also set it on the retry executor
	if s.retryExecutor != nil {
		s.retryExecutor.SetMetricsRecorder(recorder)
	}
}

func (s *Scheduler) AddJob(j Job) error {
	if j.GetSchedule() == "" {
		return ErrEmptySchedule
	}

	id, err := s.cron.AddJob(j.GetSchedule(), &jobWrapper{s, j})
	if err != nil {
		s.Logger.Warningf(
			"Failed to register job %q - %q - %q",
			j.GetName(), j.GetCommand(), j.GetSchedule(),
		)
		return fmt.Errorf("add cron job: %w", err)
	}
	j.SetCronJobID(int(id)) // Cast to int in order to avoid pushing cron external to common
	j.Use(s.Middlewares()...)
	s.Jobs = append(s.Jobs, j)
	s.Logger.Noticef(
		"New job registered %q - %q - %q - ID: %v",
		j.GetName(), j.GetCommand(), j.GetSchedule(), id,
	)
	return nil
}

func (s *Scheduler) RemoveJob(j Job) error {
	s.Logger.Noticef(
		"Job deregistered (will not fire again) %q - %q - %q - ID: %v",
		j.GetName(), j.GetCommand(), j.GetSchedule(), j.GetCronJobID(),
	)
	s.cron.Remove(cron.EntryID(j.GetCronJobID()))
	for i, job := range s.Jobs {
		if job == j || job.GetCronJobID() == j.GetCronJobID() {
			s.Jobs = append(s.Jobs[:i], s.Jobs[i+1:]...)
			break
		}
	}
	s.mu.Lock()
	s.Removed = append(s.Removed, j)
	s.mu.Unlock()
	return nil
}

func (s *Scheduler) Start() error {
	s.mu.Lock()
	s.isRunning = true

	// Build dependency graph
	if err := s.workflowOrchestrator.BuildDependencyGraph(s.Jobs); err != nil {
		s.Logger.Errorf("Failed to build dependency graph: %v", err)
		// Continue anyway - jobs without dependencies will still work
	}

	// Build job name lookup map
	for _, j := range s.Jobs {
		s.jobsByName[j.GetName()] = j
	}

	// Start workflow cleanup routine
	s.startWorkflowCleanup()

	s.mu.Unlock()
	s.Logger.Debugf("Starting scheduler")
	s.cron.Start()
	return nil
}

func (s *Scheduler) Stop() error {
	s.cron.Stop() // Stop cron first to prevent new jobs

	s.mu.Lock()
	s.isRunning = false

	// Stop cleanup routine
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
		close(s.cleanupStop)
	}
	s.mu.Unlock()

	s.wg.Wait() // Then wait for existing jobs
	return nil
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

	s.cleanupTicker = time.NewTicker(cleanupInterval)

	go func() {
		for {
			select {
			case <-s.cleanupTicker.C:
				s.workflowOrchestrator.CleanupOldExecutions(retentionDuration)
				s.Logger.Debugf("Cleaned up workflow executions older than %v", retentionDuration)
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

// RunJob manually triggers a job by name
func (s *Scheduler) RunJob(jobName string) error {
	s.mu.RLock()
	job, exists := s.jobsByName[jobName]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("job %s not found", jobName)
	}

	// Check if job can run based on dependencies
	executionID := fmt.Sprintf("manual-%d", time.Now().Unix())
	if !s.workflowOrchestrator.CanExecute(jobName, executionID) {
		return fmt.Errorf("job %s cannot run: dependencies not satisfied", jobName)
	}

	// Run the job
	wrapper := &jobWrapper{s: s, j: job}
	go wrapper.Run()

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
	j, _ := getJob(s.Jobs, name)
	return j
}

// GetDisabledJob returns a disabled job by name.
func (s *Scheduler) GetDisabledJob(name string) Job {
	j, _ := getJob(s.Disabled, name)
	return j
}

// DisableJob stops scheduling the job but keeps it for later enabling.
func (s *Scheduler) DisableJob(name string) error {
	j, idx := getJob(s.Jobs, name)
	if j == nil {
		return fmt.Errorf("job %q not found", name)
	}
	s.cron.Remove(cron.EntryID(j.GetCronJobID()))
	s.Jobs = append(s.Jobs[:idx], s.Jobs[idx+1:]...)
	s.Disabled = append(s.Disabled, j)
	return nil
}

// EnableJob schedules a previously disabled job.
func (s *Scheduler) EnableJob(name string) error {
	j, idx := getJob(s.Disabled, name)
	if j == nil {
		return fmt.Errorf("job %q not found", name)
	}
	s.Disabled = append(s.Disabled[:idx], s.Disabled[idx+1:]...)
	return s.AddJob(j)
}

// jobWrapper wraps a Job to manage running and waiting via the Scheduler.

// IsRunning returns true if the scheduler is active.
func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRunning
}

type jobWrapper struct {
	s *Scheduler
	j Job
}

func (w *jobWrapper) Run() {
	// Generate workflow execution ID
	executionID := fmt.Sprintf("sched-%d-%s", time.Now().Unix(), w.j.GetName())

	// Check dependencies
	if !w.s.workflowOrchestrator.CanExecute(w.j.GetName(), executionID) {
		w.s.Logger.Debugf("Job %q skipped - dependencies not satisfied", w.j.GetName())
		return
	}

	// Acquire semaphore slot for job concurrency limit
	select {
	case w.s.jobSemaphore <- struct{}{}:
		// Got a slot, proceed
		defer func() { <-w.s.jobSemaphore }() // Release slot when done
	default:
		// No slots available, skip this execution
		w.s.Logger.Warningf("Job %q skipped - max concurrent jobs limit reached (%d)",
			w.j.GetName(), w.s.maxConcurrentJobs)
		return
	}

	w.s.mu.Lock()
	if !w.s.isRunning {
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
		w.s.Logger.Errorf("failed to create execution: %v", err)
		return
	}

	// Ensure buffers are returned to pool when done
	defer e.Cleanup()

	ctx := NewContext(w.s, w.j, e)

	// Mark job as started in workflow
	w.s.workflowOrchestrator.JobStarted(w.j.GetName(), executionID)

	w.start(ctx)

	// Execute with retry logic
	err = w.s.retryExecutor.ExecuteWithRetry(w.j, ctx, func(c *Context) error {
		return c.Next()
	})

	w.stop(ctx, err)

	// Mark job as completed in workflow
	success := err == nil && !ctx.Execution.Failed
	w.s.workflowOrchestrator.JobCompleted(w.j.GetName(), executionID, success)
}

func (w *jobWrapper) start(ctx *Context) {
	ctx.Start()
	ctx.Log("Started - " + ctx.Job.GetCommand())

	// Record job started metric if available
	if w.s.metricsRecorder != nil {
		// This could be extended to record job start metrics
		// For now, the retry metrics are the main focus
	}
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
