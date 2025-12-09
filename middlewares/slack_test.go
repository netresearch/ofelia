package middlewares

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/netresearch/ofelia/core"

	. "gopkg.in/check.v1"
)

type SuiteSlack struct {
	BaseSuite
}

var _ = Suite(&SuiteSlack{})

func (s *SuiteSlack) TestNewSlackEmpty(c *C) {
	c.Assert(NewSlack(&SlackConfig{}), IsNil)
}

func (s *SuiteSlack) TestRunSuccess(c *C) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m slackMessage
		_ = json.Unmarshal([]byte(r.FormValue(slackPayloadVar)), &m)
		c.Assert(m.Attachments[0].Title, Equals, "Execution successful")
	}))

	defer ts.Close()

	s.ctx.Start()
	s.ctx.Stop(nil)

	m := NewSlack(&SlackConfig{SlackWebhook: ts.URL})
	c.Assert(m.Run(s.ctx), IsNil)
}

func (s *SuiteSlack) TestRunSuccessFailed(c *C) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m slackMessage
		_ = json.Unmarshal([]byte(r.FormValue(slackPayloadVar)), &m)
		c.Assert(m.Attachments[0].Title, Equals, "Execution failed")
	}))

	defer ts.Close()

	s.ctx.Start()
	s.ctx.Stop(errors.New("foo"))

	m := NewSlack(&SlackConfig{SlackWebhook: ts.URL})
	c.Assert(m.Run(s.ctx), IsNil)
}

func (s *SuiteSlack) TestRunSuccessOnError(c *C) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(true, Equals, false)
	}))

	defer ts.Close()

	s.ctx.Start()
	s.ctx.Stop(nil)

	m := NewSlack(&SlackConfig{SlackWebhook: ts.URL, SlackOnlyOnError: true})
	c.Assert(m.Run(s.ctx), IsNil)
}

func (s *SuiteSlack) TestCustomHTTPClient(c *C) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m slackMessage
		_ = json.Unmarshal([]byte(r.FormValue(slackPayloadVar)), &m)
		c.Assert(m.Attachments[0].Title, Equals, "Execution successful")
	}))

	defer ts.Close()

	s.ctx.Start()
	s.ctx.Stop(nil)

	m := NewSlack(&SlackConfig{SlackWebhook: ts.URL}).(*Slack)
	custom := ts.Client()
	custom.Timeout = 2 * time.Second
	m.Client = custom

	c.Assert(m.Run(s.ctx), IsNil)
}

func (s *SuiteSlack) TestDedupSuppressesDuplicateErrors(c *C) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
	}))
	defer ts.Close()

	// Create dedup with 1 hour cooldown
	dedup := NewNotificationDedup(time.Hour)

	// Create fresh context for this test
	job := &TestJob{}
	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	c.Assert(err, IsNil)
	ctx := core.NewContext(sh, job, e)

	// First error - should send notification
	ctx.Start()
	ctx.Stop(errors.New("test error"))

	m := NewSlack(&SlackConfig{SlackWebhook: ts.URL, Dedup: dedup}).(*Slack)
	c.Assert(m.Run(ctx), IsNil)
	c.Assert(callCount, Equals, 1)

	// Create new execution for second run
	e2, _ := core.NewExecution()
	ctx2 := core.NewContext(sh, job, e2)
	ctx2.Start()
	ctx2.Stop(errors.New("test error"))
	c.Assert(m.Run(ctx2), IsNil)
	c.Assert(callCount, Equals, 1) // Still 1, not 2 (suppressed)

	// Different error - should send notification
	e3, _ := core.NewExecution()
	ctx3 := core.NewContext(sh, job, e3)
	ctx3.Start()
	ctx3.Stop(errors.New("different error"))
	c.Assert(m.Run(ctx3), IsNil)
	c.Assert(callCount, Equals, 2) // Now 2
}

func (s *SuiteSlack) TestDedupAllowsSuccessNotifications(c *C) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
	}))
	defer ts.Close()

	dedup := NewNotificationDedup(time.Hour)

	// Create fresh context
	job := &TestJob{}
	sh := core.NewScheduler(&TestLogger{})
	e, _ := core.NewExecution()
	ctx := core.NewContext(sh, job, e)

	// Success - should always send (dedup only applies to errors)
	ctx.Start()
	ctx.Stop(nil)

	m := NewSlack(&SlackConfig{SlackWebhook: ts.URL, Dedup: dedup}).(*Slack)
	c.Assert(m.Run(ctx), IsNil)
	c.Assert(callCount, Equals, 1)

	// Another success - should also send
	e2, _ := core.NewExecution()
	ctx2 := core.NewContext(sh, job, e2)
	ctx2.Start()
	ctx2.Stop(nil)
	c.Assert(m.Run(ctx2), IsNil)
	c.Assert(callCount, Equals, 2)
}

func (s *SuiteSlack) TestNoDedupWhenNotConfigured(c *C) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
	}))
	defer ts.Close()

	// Create fresh context
	job := &TestJob{}
	sh := core.NewScheduler(&TestLogger{})
	e, _ := core.NewExecution()
	ctx := core.NewContext(sh, job, e)

	// No dedup configured
	m := NewSlack(&SlackConfig{SlackWebhook: ts.URL}).(*Slack)

	// First error
	ctx.Start()
	ctx.Stop(errors.New("test error"))
	c.Assert(m.Run(ctx), IsNil)
	c.Assert(callCount, Equals, 1)

	// Same error again - should still send (no dedup)
	e2, _ := core.NewExecution()
	ctx2 := core.NewContext(sh, job, e2)
	ctx2.Start()
	ctx2.Stop(errors.New("test error"))
	c.Assert(m.Run(ctx2), IsNil)
	c.Assert(callCount, Equals, 2)
}
