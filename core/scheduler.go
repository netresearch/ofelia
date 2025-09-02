package core

import (
	"errors"
	"fmt"
	"sync"

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
	cron             *cron.Cron
	wg               sync.WaitGroup
	isRunning        bool
	mu               sync.RWMutex // Protect isRunning and wg/removed operations
	maxConcurrentJobs int
	jobSemaphore     chan struct{} // Limits concurrent job execution
	retryExecutor    *RetryExecutor
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
	
	return &Scheduler{
		Logger:            l,
		cron:              cron,
		maxConcurrentJobs: maxConcurrent,
		jobSemaphore:      make(chan struct{}, maxConcurrent),
		retryExecutor:     NewRetryExecutor(l),
	}
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
	s.mu.Unlock()
	s.Logger.Debugf("Starting scheduler")
	s.cron.Start()
	return nil
}

func (s *Scheduler) Stop() error {
	s.cron.Stop() // Stop cron first to prevent new jobs

	s.mu.Lock()
	s.isRunning = false
	s.mu.Unlock()

	s.wg.Wait() // Then wait for existing jobs
	return nil
}

// Entries returns all scheduled cron entries.
func (s *Scheduler) Entries() []cron.Entry {
	return s.cron.Entries()
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

// RunJob manually executes a job by name.
func (s *Scheduler) RunJob(name string) error {
	j, _ := getJob(s.Jobs, name)
	if j == nil {
		return fmt.Errorf("job %q not found", name)
	}
	go (&jobWrapper{s: s, j: j}).Run()
	return nil
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

	w.start(ctx)
	
	// Execute with retry logic
	err = w.s.retryExecutor.ExecuteWithRetry(w.j, ctx, func(c *Context) error {
		return c.Next()
	})
	
	w.stop(ctx, err)
}

func (w *jobWrapper) start(ctx *Context) {
	ctx.Start()
	ctx.Log("Started - " + ctx.Job.GetCommand())
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
