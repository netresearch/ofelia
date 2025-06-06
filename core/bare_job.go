package core

import (
	"reflect"
	"sync"
	"sync/atomic"
)

type BareJob struct {
	Schedule     string `hash:"true"`
	Name         string `hash:"true"`
	Command      string `hash:"true"`
	HistoryLimit int    `default:"10"`

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
	if err := getHash(reflect.TypeOf(j).Elem(), reflect.ValueOf(j).Elem(), &hash); err != nil {
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
