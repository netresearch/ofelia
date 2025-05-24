package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netresearch/ofelia/core"
	. "gopkg.in/check.v1"
)

type SuiteServer struct{}

var _ = Suite(&SuiteServer{})

func Test(t *testing.T) { TestingT(t) }

// minimal job used for server tests
type testJob struct{ core.BareJob }

func (j *testJob) Run(ctx *core.Context) error { return nil }

type testLogger struct{}

func (*testLogger) Criticalf(string, ...interface{}) {}
func (*testLogger) Debugf(string, ...interface{})    {}
func (*testLogger) Errorf(string, ...interface{})    {}
func (*testLogger) Noticef(string, ...interface{})   {}
func (*testLogger) Warningf(string, ...interface{})  {}

func (s *SuiteServer) TestJobHistoryHandler(c *C) {
	sc := core.NewScheduler(&testLogger{})
	job := &testJob{core.BareJob{Name: "foo", Schedule: "@hourly", Command: "echo"}}
	c.Assert(sc.AddJob(job), IsNil)

	e, err := core.NewExecution()
	c.Assert(err, IsNil)
	e.Start()
	e.OutputStream.Write([]byte("out"))
	e.ErrorStream.Write([]byte("err"))
	e.Stop(nil)
	job.SetLastRun(e)

	srv := NewServer(":0", sc)
	req := httptest.NewRequest("GET", "/api/jobs/foo/history", nil)
	w := httptest.NewRecorder()
	srv.jobHistoryHandler(w, req)

	c.Assert(w.Code, Equals, http.StatusOK)
	var hist []apiExecution
	c.Assert(json.Unmarshal(w.Body.Bytes(), &hist), IsNil)
	c.Assert(len(hist), Equals, 1)
	c.Assert(hist[0].Stdout, Equals, "out")
	c.Assert(hist[0].Stderr, Equals, "err")
}

func (s *SuiteServer) TestJobHistoryHandlerNotFound(c *C) {
	sc := core.NewScheduler(&testLogger{})
	srv := NewServer(":0", sc)
	req := httptest.NewRequest("GET", "/api/jobs/unknown/history", nil)
	w := httptest.NewRecorder()
	srv.jobHistoryHandler(w, req)
	c.Assert(w.Code, Equals, http.StatusNotFound)
}
