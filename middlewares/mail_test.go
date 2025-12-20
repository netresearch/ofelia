package middlewares

import (
	"bytes"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	smtp "github.com/emersion/go-smtp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

type mailTestFixture struct {
	ctx       *core.Context
	job       *TestJob
	l         net.Listener
	server    *smtp.Server
	smtpdHost string
	smtpdPort int
	fromCh    chan string
	dataCh    chan string
}

func setupMailTest(t *testing.T) *mailTestFixture {
	t.Helper()

	ctx, job := setupTestContext(t)

	fromCh := make(chan string, 1)
	dataCh := make(chan string, 1)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := smtp.NewServer(&testBackend{fromCh: fromCh, dataCh: dataCh})
	srv.AllowInsecureAuth = true

	go func(srv *smtp.Server, ln net.Listener) {
		err := srv.Serve(ln)
		if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			t.Logf("SMTP server error: %v", err)
		}
	}(srv, ln)

	p := strings.Split(ln.Addr().String(), ":")
	port, _ := strconv.Atoi(p[1])

	t.Cleanup(func() {
		ln.Close()
	})

	return &mailTestFixture{
		ctx:       ctx,
		job:       job,
		l:         ln,
		server:    srv,
		smtpdHost: p[0],
		smtpdPort: port,
		fromCh:    fromCh,
		dataCh:    dataCh,
	}
}

func TestNewMailEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, NewMail(&MailConfig{}))
}

func TestMailRunSuccess(t *testing.T) {
	t.Parallel()
	f := setupMailTest(t)

	f.ctx.Start()
	f.ctx.Stop(nil)

	m := NewMail(&MailConfig{
		SMTPHost:  f.smtpdHost,
		SMTPPort:  f.smtpdPort,
		EmailTo:   "foo@foo.com",
		EmailFrom: "qux@qux.com",
	})

	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = m.Run(f.ctx)
		close(done)
	}()

	select {
	case from := <-f.fromCh:
		assert.Equal(t, "qux@qux.com", from)
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for SMTP server to receive MAIL FROM")
	}

	<-done
	require.NoError(t, runErr)
}

func TestMailRunWithEmptyStreams(t *testing.T) {
	t.Parallel()
	f := setupMailTest(t)

	f.ctx.Start()
	f.ctx.Stop(nil)

	assert.Equal(t, int64(0), f.ctx.Execution.OutputStream.TotalWritten())
	assert.Equal(t, int64(0), f.ctx.Execution.ErrorStream.TotalWritten())

	m := NewMail(&MailConfig{
		SMTPHost:  f.smtpdHost,
		SMTPPort:  f.smtpdPort,
		EmailTo:   "foo@foo.com",
		EmailFrom: "qux@qux.com",
	})

	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = m.Run(f.ctx)
		close(done)
	}()

	select {
	case from := <-f.fromCh:
		assert.Equal(t, "qux@qux.com", from)
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for SMTP server to receive MAIL FROM")
	}

	select {
	case emailData := <-f.dataCh:
		assert.NotContains(t, emailData, "stdout.log",
			"stdout.log attachment should not be included for empty streams")
		assert.NotContains(t, emailData, "stderr.log",
			"stderr.log attachment should not be included for empty streams")
		assert.Contains(t, emailData, ".json",
			"JSON attachment with job metadata should always be included")
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for email data")
	}

	<-done
	require.NoError(t, runErr)
}

func TestMailRunWithNonEmptyStreams(t *testing.T) {
	t.Parallel()
	f := setupMailTest(t)

	f.ctx.Start()
	_, _ = f.ctx.Execution.OutputStream.Write([]byte("stdout content"))
	_, _ = f.ctx.Execution.ErrorStream.Write([]byte("stderr content"))
	f.ctx.Stop(nil)

	assert.Positive(t, f.ctx.Execution.OutputStream.TotalWritten())
	assert.Positive(t, f.ctx.Execution.ErrorStream.TotalWritten())

	m := NewMail(&MailConfig{
		SMTPHost:  f.smtpdHost,
		SMTPPort:  f.smtpdPort,
		EmailTo:   "foo@foo.com",
		EmailFrom: "qux@qux.com",
	})

	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = m.Run(f.ctx)
		close(done)
	}()

	select {
	case from := <-f.fromCh:
		assert.Equal(t, "qux@qux.com", from)
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for SMTP server to receive MAIL FROM")
	}

	select {
	case emailData := <-f.dataCh:
		assert.Contains(t, emailData, "stdout.log",
			"stdout.log attachment should be included for non-empty streams")
		assert.Contains(t, emailData, "stderr.log",
			"stderr.log attachment should be included for non-empty streams")
		assert.Contains(t, emailData, ".json",
			"JSON attachment with job metadata should always be included")
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for email data")
	}

	<-done
	require.NoError(t, runErr)
}

type testBackend struct {
	fromCh chan string
	dataCh chan string
}

func (b *testBackend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &testSession{fromCh: b.fromCh, dataCh: b.dataCh}, nil
}

type testSession struct {
	fromCh chan string
	dataCh chan string
}

func (s *testSession) Mail(from string, _ *smtp.MailOptions) error {
	s.fromCh <- from
	return nil
}

func (s *testSession) Rcpt(_ string, _ *smtp.RcptOptions) error { return nil }

func (s *testSession) Data(r io.Reader) error {
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if s.dataCh != nil {
		s.dataCh <- buf.String()
	}
	return nil
}

func (s *testSession) Reset()        {}
func (s *testSession) Logout() error { return nil }

func TestMailCustomEmailSubject(t *testing.T) {
	t.Parallel()
	f := setupMailTest(t)

	f.ctx.Start()
	f.ctx.Stop(nil)

	m := NewMail(&MailConfig{
		SMTPHost:     f.smtpdHost,
		SMTPPort:     f.smtpdPort,
		EmailTo:      "foo@foo.com",
		EmailFrom:    "qux@qux.com",
		EmailSubject: "[CUSTOM] Job {{.Job.GetName}} - {{status .Execution}}",
	})

	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = m.Run(f.ctx)
		close(done)
	}()

	select {
	case <-f.fromCh:
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for SMTP server to receive MAIL FROM")
	}

	select {
	case emailData := <-f.dataCh:
		assert.Contains(t, emailData, "Subject: [CUSTOM]",
			"Custom subject prefix should be present")
		assert.Contains(t, emailData, f.ctx.Job.GetName(),
			"Job name should be in subject")
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for email data")
	}

	<-done
	require.NoError(t, runErr)
}

func TestMailDefaultEmailSubject(t *testing.T) {
	t.Parallel()
	f := setupMailTest(t)

	f.ctx.Start()
	f.ctx.Stop(nil)

	m := NewMail(&MailConfig{
		SMTPHost:  f.smtpdHost,
		SMTPPort:  f.smtpdPort,
		EmailTo:   "foo@foo.com",
		EmailFrom: "qux@qux.com",
	})

	done := make(chan struct{})
	var runErr2 error
	go func() {
		runErr2 = m.Run(f.ctx)
		close(done)
	}()

	select {
	case <-f.fromCh:
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for SMTP server to receive MAIL FROM")
	}

	select {
	case emailData := <-f.dataCh:
		assert.Contains(t, emailData, "Execution",
			"Default subject should contain 'Execution'")
		assert.Contains(t, emailData, f.ctx.Job.GetName(),
			"Default subject should contain job name")
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for email data")
	}

	<-done
	require.NoError(t, runErr2)
}
