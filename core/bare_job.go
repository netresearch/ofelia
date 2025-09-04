package core

import (
	"reflect"
	"sync"
	"sync/atomic"
)

type BareJob struct {
	Schedule         string   `hash:"true"`
	Name             string   `hash:"true"`
	Command          string   `hash:"true"`
	HistoryLimit     int      `default:"10"`
	MaxRetries       int      `default:"0"`     // Maximum number of retry attempts (0 = no retries)
	RetryDelayMs     int      `default:"1000"`  // Initial retry delay in milliseconds
	RetryExponential bool     `default:"true"`  // Use exponential backoff for retries
	RetryMaxDelayMs  int      `default:"60000"` // Maximum retry delay in milliseconds (1 minute)
	Dependencies     []string // Names of jobs that must complete successfully before this job
	OnSuccess        []string // Jobs to trigger on successful completion
	OnFailure        []string // Jobs to trigger on failure
	AllowParallel    bool     `default:"true"` // Allow job to run in parallel with others

	middlewareContainer
	running int32
	lock    sync.Mutex
	history []*Execution
	lastRun *Execution
	cronID  int
}

func (j *BareJob) GetName() string {
	return j.Name
}

func (j *BareJob) GetSchedule() string {
	return j.Schedule
}

func (j *BareJob) GetCommand() string {
	return j.Command
}

func (j *BareJob) Running() int32 {
	return atomic.LoadInt32(&j.running)
}

func (j *BareJob) NotifyStart() {
	atomic.AddInt32(&j.running, 1)
}

func (j *BareJob) NotifyStop() {
	atomic.AddInt32(&j.running, -1)
}

func (j *BareJob) GetCronJobID() int {
	return j.cronID
}

func (j *BareJob) SetCronJobID(id int) {
	j.cronID = id
}

// Returns a hash of all the job attributes. Used to detect changes
func (j *BareJob) Hash() (string, error) {
	var hash string
	if err := GetHash(reflect.TypeOf(j).Elem(), reflect.ValueOf(j).Elem(), &hash); err != nil {
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
	return ctx.Next()
}
