package core

import (
	"errors"
	"fmt"
	"sync"

	"github.com/robfig/cron/v3"
)

var (
	ErrEmptyScheduler = errors.New("unable to start a empty scheduler.")
	ErrEmptySchedule  = errors.New("unable to add a job with a empty schedule.")
)

type Scheduler struct {
	Jobs   []Job
	Logger Logger

	middlewareContainer
	cron      *cron.Cron
	wg        sync.WaitGroup
	isRunning bool
	mu        sync.RWMutex // Protect isRunning and wg operations
}

func NewScheduler(l Logger) *Scheduler {
	cronUtils := NewCronUtils(l)
	cron := cron.New(
		cron.WithParser(cron.NewParser(cron.SecondOptional|cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow|cron.Descriptor)),
		cron.WithLogger(cronUtils),
		cron.WithChain(cron.Recover(cronUtils)),
	)

	return &Scheduler{
		Logger: l,
		cron:   cron,
	}
}

func (s *Scheduler) AddJob(j Job) error {
	if j.GetSchedule() == "" {
		return ErrEmptySchedule
	}

	id, err := s.cron.AddJob(j.GetSchedule(), &jobWrapper{s, j})
	if err != nil {
		s.Logger.Warningf("Failed to register job %q - %q - %q", j.GetName(), j.GetCommand(), j.GetSchedule())
		return err
	}
	j.SetCronJobID(int(id)) // Cast to int in order to avoid pushing cron external to common
	j.Use(s.Middlewares()...)
	s.Logger.Noticef("New job registered %q - %q - %q - ID: %v", j.GetName(), j.GetCommand(), j.GetSchedule(), id)
	return nil
}

func (s *Scheduler) RemoveJob(j Job) error {
	s.Logger.Noticef("Job deregistered (will not fire again) %q - %q - %q - ID: %v", j.GetName(), j.GetCommand(), j.GetSchedule(), j.GetCronJobID())
	s.cron.Remove(cron.EntryID(j.GetCronJobID()))
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

	e := NewExecution()
	ctx := NewContext(w.s, w.j, e)

	w.start(ctx)
	err := ctx.Next()
	w.stop(ctx, err)
}

func (w *jobWrapper) start(ctx *Context) {
	ctx.Start()
	ctx.Log("Started - " + ctx.Job.GetCommand())
}

func (w *jobWrapper) stop(ctx *Context, err error) {
	ctx.Stop(err)

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
