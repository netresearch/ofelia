package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBareJobGetters(t *testing.T) {
	t.Parallel()

	job := &BareJob{
		Name:     "foo",
		Schedule: "bar",
		Command:  "qux",
	}

	assert.Equal(t, "foo", job.GetName())
	assert.Equal(t, "bar", job.GetSchedule())
	assert.Equal(t, "qux", job.GetCommand())
}

func TestBareJobNotifyStartStop(t *testing.T) {
	t.Parallel()

	job := &BareJob{}

	job.NotifyStart()
	assert.Equal(t, int32(1), job.Running())

	job.NotifyStop()
	assert.Equal(t, int32(0), job.Running())
}

func TestBareJobHistoryTruncation(t *testing.T) {
	t.Parallel()

	job := &BareJob{HistoryLimit: 2}
	e1, e2, e3 := &Execution{}, &Execution{}, &Execution{}
	job.SetLastRun(e1)
	job.SetLastRun(e2)
	job.SetLastRun(e3)

	assert.Len(t, job.history, 2)
	assert.Equal(t, e2, job.history[0])
	assert.Equal(t, e3, job.history[1])
}

func TestBareJobHistoryUnlimited(t *testing.T) {
	t.Parallel()

	job := &BareJob{}
	job.SetLastRun(&Execution{})
	job.SetLastRun(&Execution{})

	assert.Len(t, job.history, 2)
}

func TestBareJobGetHistory(t *testing.T) {
	t.Parallel()

	job := &BareJob{}
	e1, e2 := &Execution{ID: "1"}, &Execution{ID: "2"}
	job.SetLastRun(e1)
	job.SetLastRun(e2)

	hist := job.GetHistory()
	assert.Len(t, hist, 2)
	assert.Equal(t, e1, hist[0])
	assert.Equal(t, e2, hist[1])

	hist[0] = nil
	assert.Equal(t, e1, job.history[0])
}
