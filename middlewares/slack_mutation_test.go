package middlewares

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

// TestSlackPushMessage_NilClient kills CONDITIONALS_NEGATION at slack.go:90
// The condition is: if m.Client == nil { m.Client = &http.Client{...} }
// Negating to m.Client != nil would overwrite an existing client with a new one,
// and leave nil when it IS nil (causing a nil pointer dereference).
func TestSlackPushMessage_NilClient(t *testing.T) {
	t.Parallel()

	var called int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create Slack middleware with nil Client
	m := &Slack{
		SlackConfig: SlackConfig{SlackWebhook: ts.URL},
		Client:      nil, // explicitly nil
	}

	job := &TestJob{}
	job.Name = "test-job"
	job.Command = "echo hello"
	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	require.NoError(t, err)
	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	// pushMessage should create a client and send successfully
	m.pushMessage(ctx)

	// Verify that the client was created (not nil anymore)
	assert.NotNil(t, m.Client, "pushMessage should create a client when Client is nil")
	assert.Equal(t, int32(1), atomic.LoadInt32(&called),
		"pushMessage should succeed even with initially nil client")
}

// TestSlackPushMessage_ExistingClient kills CONDITIONALS_NEGATION at slack.go:90
// If the condition is negated, an existing client would be overwritten.
// We verify that an existing custom client is preserved and used.
func TestSlackPushMessage_ExistingClient(t *testing.T) {
	t.Parallel()

	var called int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	customClient := ts.Client()
	customClient.Timeout = 42 * time.Second

	m := &Slack{
		SlackConfig: SlackConfig{SlackWebhook: ts.URL},
		Client:      customClient,
	}

	job := &TestJob{}
	job.Name = "test-job"
	job.Command = "echo hello"
	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	require.NoError(t, err)
	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	m.pushMessage(ctx)

	// Verify the original client is preserved (not overwritten)
	assert.Equal(t, 42*time.Second, m.Client.Timeout,
		"existing client should not be overwritten")
	assert.Equal(t, int32(1), atomic.LoadInt32(&called))
}

// TestSlackPushMessage_HTTPError kills CONDITIONALS_NEGATION at slack.go:109
// The condition is: if err != nil { log error } else { check status }
// Negating to err == nil would log error on success and try to read body on error.
func TestSlackPushMessage_HTTPError(t *testing.T) {
	t.Parallel()

	// Use a server that is immediately closed to force a connection error
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	closedURL := ts.URL
	ts.Close() // Close immediately to force connection error

	m := &Slack{
		SlackConfig: SlackConfig{SlackWebhook: closedURL},
		Client:      &http.Client{Timeout: 1 * time.Second},
	}

	job := &TestJob{}
	job.Name = "test-job"
	job.Command = "echo hello"
	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	require.NoError(t, err)
	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	// This should not panic; if the condition is negated, it would try to
	// access r.Body on a nil response (since Client.Do returned an error).
	// The original code logs the error and returns.
	assert.NotPanics(t, func() {
		m.pushMessage(ctx)
	})
}

// TestSlackPushMessage_Non200Status kills CONDITIONALS_NEGATION at slack.go:113
// The condition is: if r.StatusCode != http.StatusOK { log error }
// Negating to r.StatusCode == http.StatusOK would log error on 200 but not on 500.
func TestSlackPushMessage_Non200Status(t *testing.T) {
	t.Parallel()

	var called int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	// Use a logger that captures errors to verify the error is logged
	logger := &capturingLogger{}

	m := &Slack{
		SlackConfig: SlackConfig{SlackWebhook: ts.URL},
		Client:      &http.Client{Timeout: 5 * time.Second},
	}

	job := &TestJob{}
	job.Name = "test-job"
	job.Command = "echo hello"
	sh := core.NewScheduler(logger)
	e, err := core.NewExecution()
	require.NoError(t, err)
	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	m.pushMessage(ctx)

	assert.Equal(t, int32(1), atomic.LoadInt32(&called))
	// The logger should have received an error about non-200
	assert.NotEmpty(t, logger.errors, "non-200 status should be logged as error")
	assert.Contains(t, logger.errors[0], "non-200",
		"error message should mention non-200 status")
}

// TestSlackPushMessage_200Status verifies success path does not log errors.
func TestSlackPushMessage_200StatusNoError(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	logger := &capturingLogger{}

	m := &Slack{
		SlackConfig: SlackConfig{SlackWebhook: ts.URL},
		Client:      &http.Client{Timeout: 5 * time.Second},
	}

	job := &TestJob{}
	job.Name = "test-job"
	job.Command = "echo hello"
	sh := core.NewScheduler(logger)
	e, err := core.NewExecution()
	require.NoError(t, err)
	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	m.pushMessage(ctx)

	// On 200 OK, no error should be logged
	assert.Empty(t, logger.errors, "200 OK should not produce error logs")
}

// TestSlackBuildMessage_FailedExecution verifies failed execution message content.
func TestSlackBuildMessage_FailedExecution(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg slackMessage
		err := json.Unmarshal([]byte(r.FormValue(slackPayloadVar)), &msg)
		assert.NoError(t, err)
		assert.Equal(t, "Execution failed", msg.Attachments[0].Title)
		assert.Equal(t, "#F35A00", msg.Attachments[0].Color)
		assert.Contains(t, msg.Attachments[0].Text, "test failure")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	m := &Slack{
		SlackConfig: SlackConfig{SlackWebhook: ts.URL},
		Client:      ts.Client(),
	}

	job := &TestJob{}
	job.Name = "failing-job"
	job.Command = "exit 1"
	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	require.NoError(t, err)
	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(errors.New("test failure"))

	m.pushMessage(ctx)
}

// TestSlackBuildMessage_SkippedExecution verifies skipped execution message.
func TestSlackBuildMessage_SkippedExecution(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg slackMessage
		err := json.Unmarshal([]byte(r.FormValue(slackPayloadVar)), &msg)
		assert.NoError(t, err)
		assert.Equal(t, "Execution skipped", msg.Attachments[0].Title)
		assert.Equal(t, "#FFA500", msg.Attachments[0].Color)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	m := &Slack{
		SlackConfig: SlackConfig{SlackWebhook: ts.URL},
		Client:      ts.Client(),
	}

	job := &TestJob{}
	job.Name = "skipped-job"
	job.Command = "echo skip"
	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	require.NoError(t, err)
	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(core.ErrSkippedExecution)

	m.pushMessage(ctx)
}

// capturingLogger captures log messages for assertions.
type capturingLogger struct {
	errors []string
	debugs []string
}

func (l *capturingLogger) Criticalf(format string, args ...any) {}
func (l *capturingLogger) Debugf(format string, args ...any) {
	l.debugs = append(l.debugs, fmt.Sprintf(format, args...))
}

func (l *capturingLogger) Errorf(format string, args ...any) {
	l.errors = append(l.errors, fmt.Sprintf(format, args...))
}
func (l *capturingLogger) Noticef(format string, args ...any)  {}
func (l *capturingLogger) Warningf(format string, args ...any) {}
