package middlewares

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

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
		json.Unmarshal([]byte(r.FormValue(slackPayloadVar)), &m)
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
		json.Unmarshal([]byte(r.FormValue(slackPayloadVar)), &m)
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
		json.Unmarshal([]byte(r.FormValue(slackPayloadVar)), &m)
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
