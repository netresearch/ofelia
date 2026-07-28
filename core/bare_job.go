// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"reflect"
	"sync"
	"sync/atomic"
)

// BareJob is the base type embedded by every job implementation (ExecJob,
// RunJob, RunServiceJob, LocalJob, ComposeJob). It holds the fields the
// scheduler reads — name, schedule, command, retry and dependency settings —
// together with the middleware chain, the in-flight counter and a bounded
// execution history, and supplies all of the Job interface except Run through
// promoted methods. Embedders override Run to do the actual work.
//
// Field defaults come from the `default` struct tags applied during config
// parsing. On a hand-built value HistoryLimit is 0, which keeps the history
// unbounded.
type BareJob struct {
	Schedule string `hash:"true"`
	Name     string `hash:"true"`
	Command  string `hash:"true"`
	// RunOnStartup controls whether the job is executed once immediately when the scheduler starts,
	// before regular cron-based scheduling begins. This is a boolean flag with a default value of false.
	// Startup executions are dispatched in non-blocking goroutines so they do not delay scheduler startup.
	RunOnStartup     bool     `default:"false" gcfg:"run-on-startup" mapstructure:"run-on-startup" hash:"true"`
	HistoryLimit     int      `default:"10"`
	MaxRetries       int      `default:"0"`                                  // Maximum number of retry attempts (0 = no retries)
	RetryDelayMs     int      `default:"1000"`                               // Initial retry delay in milliseconds
	RetryExponential bool     `default:"true"`                               // Use exponential backoff for retries
	RetryMaxDelayMs  int      `default:"60000"`                              // Maximum retry delay in milliseconds (1 minute)
	Dependencies     []string `gcfg:"depends-on" mapstructure:"depends-on,"` // Jobs that must complete first
	OnSuccess        []string `gcfg:"on-success" mapstructure:"on-success,"` // Jobs to trigger on success
	OnFailure        []string `gcfg:"on-failure" mapstructure:"on-failure,"` // Jobs to trigger on failure
	AllowParallel    bool     `default:"true"`                               // Allow job to run in parallel with others

	middlewareContainer
	running int32
	lock    sync.Mutex
	history []*Execution
	lastRun *Execution
	cronID  uint64
}

// GetName returns the job's configured name. The name is the scheduler's
// primary key: go-cron entry registration, lookup, removal, enable/disable and
// manual triggering all address a job by it.
func (j *BareJob) GetName() string {
	return j.Name
}

// GetDependencies returns the list of job names this job depends on.
func (j *BareJob) GetDependencies() []string {
	return j.Dependencies
}

// GetOnSuccess returns the list of job names to trigger when this job succeeds.
func (j *BareJob) GetOnSuccess() []string {
	return j.OnSuccess
}

// GetOnFailure returns the list of job names to trigger when this job fails.
func (j *BareJob) GetOnFailure() []string {
	return j.OnFailure
}

// GetSchedule returns the raw cron expression or keyword the job was configured
// with, unparsed. Scheduler.AddJobWithTags rejects an empty schedule; the
// @triggered, @manual and @none keywords register an entry that never fires on
// its own (see IsTriggeredSchedule).
func (j *BareJob) GetSchedule() string {
	return j.Schedule
}

// GetCommand returns the configured command string. BareJob does not interpret
// it — that is up to the embedding job type; the scheduler only uses it for log
// messages and as part of the change-detection hash.
func (j *BareJob) GetCommand() string {
	return j.Command
}

// ShouldRunOnStartup returns true if the job should run immediately when the scheduler starts.
func (j *BareJob) ShouldRunOnStartup() bool {
	return j.RunOnStartup
}

// Running returns the number of invocations currently in flight, as maintained
// by NotifyStart and NotifyStop. Safe for concurrent use.
func (j *BareJob) Running() int32 {
	return atomic.LoadInt32(&j.running)
}

// NotifyStart increments the in-flight counter. Context.Start calls it when an
// execution begins; every call must be paired with a NotifyStop or Running will
// never return to zero. Safe for concurrent use.
func (j *BareJob) NotifyStart() {
	atomic.AddInt32(&j.running, 1)
}

// NotifyStop decrements the in-flight counter. Context.Stop calls it once the
// execution has finished. Safe for concurrent use.
func (j *BareJob) NotifyStop() {
	atomic.AddInt32(&j.running, -1)
}

// GetCronJobID returns the go-cron entry ID assigned when the job was
// registered, or 0 if it has not been added to a scheduler yet.
func (j *BareJob) GetCronJobID() uint64 {
	return j.cronID
}

// SetCronJobID records the go-cron entry ID for the job.
// Scheduler.AddJobWithTags calls it right after registration. It is not
// synchronized, so it must not be called while the job is scheduled.
func (j *BareJob) SetCronJobID(id uint64) {
	j.cronID = id
}

// Hash returns a change-detection key over the job's `hash:"true"` fields —
// schedule, name, command and run-on-startup — so a reload can tell whether a
// job definition actually changed. It is a plain concatenation produced by
// GetHash, not a cryptographic digest, and returns an error if a tagged field
// has a type GetHash cannot represent. Job types with extra significant fields
// (RunJob) override this method.
func (j *BareJob) Hash() (string, error) {
	var hash string
	if err := GetHash(reflect.TypeFor[BareJob](), reflect.ValueOf(j).Elem(), &hash); err != nil {
		return "", err
	}
	return hash, nil
}

// SetLastRun stores the last executed run for the job.
func (j *BareJob) SetLastRun(e *Execution) {
	j.lock.Lock()
	defer j.lock.Unlock()
	j.lastRun = e
	j.history = append(j.history, e)
	if j.HistoryLimit > 0 && len(j.history) > j.HistoryLimit {
		j.history = j.history[len(j.history)-j.HistoryLimit:]
	}
}

// GetLastRun returns the last execution of the job, if any.
func (j *BareJob) GetLastRun() *Execution {
	j.lock.Lock()
	defer j.lock.Unlock()
	return j.lastRun
}

// GetHistory returns a copy of the job's execution history.
func (j *BareJob) GetHistory() []*Execution {
	j.lock.Lock()
	defer j.lock.Unlock()
	hist := make([]*Execution, len(j.history))
	copy(hist, j.history)
	return hist
}

// Run implements the Job interface - this is handled by jobWrapper
func (j *BareJob) Run(ctx *Context) error {
	// This method is typically not called directly
	// The scheduler's jobWrapper handles the actual execution
	// For BareJob, we don't execute anything directly - it's just a container
	// Calling ctx.Next() here would create infinite recursion when BareJob is the main job
	// So we return nil to indicate successful "execution" of this bare container
	return nil
}
