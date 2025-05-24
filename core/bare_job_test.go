package core

import . "gopkg.in/check.v1"

type SuiteBareJob struct{}

var _ = Suite(&SuiteBareJob{})

func (s *SuiteBareJob) TestGetters(c *C) {
	job := &BareJob{
		Name:     "foo",
		Schedule: "bar",
		Command:  "qux",
	}

	c.Assert(job.GetName(), Equals, "foo")
	c.Assert(job.GetSchedule(), Equals, "bar")
	c.Assert(job.GetCommand(), Equals, "qux")
}

func (s *SuiteBareJob) TestNotifyStartStop(c *C) {
	job := &BareJob{}

	job.NotifyStart()
	c.Assert(job.Running(), Equals, int32(1))

	job.NotifyStop()
	c.Assert(job.Running(), Equals, int32(0))
}

func (s *SuiteBareJob) TestHistoryTruncation(c *C) {
	job := &BareJob{HistoryLimit: 2}
	e1, e2, e3 := &Execution{}, &Execution{}, &Execution{}
	job.SetLastRun(e1)
	job.SetLastRun(e2)
	job.SetLastRun(e3)
	c.Assert(len(job.history), Equals, 2)
	c.Assert(job.history[0], Equals, e2)
	c.Assert(job.history[1], Equals, e3)
}

func (s *SuiteBareJob) TestHistoryUnlimited(c *C) {
	job := &BareJob{}
	job.SetLastRun(&Execution{})
	job.SetLastRun(&Execution{})
	c.Assert(len(job.history), Equals, 2)
}

func (s *SuiteBareJob) TestGetHistory(c *C) {
	job := &BareJob{}
	e1 := &Execution{}
	e2 := &Execution{}
	job.SetLastRun(e1)
	job.SetLastRun(e2)

	hist := job.GetHistory()
	c.Assert(len(hist), Equals, 2)
	c.Assert(hist[0], Equals, e1)
	c.Assert(hist[1], Equals, e2)
}
