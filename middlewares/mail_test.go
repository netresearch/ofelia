package middlewares

import (
	"bytes"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	smtp "github.com/emersion/go-smtp"
	. "gopkg.in/check.v1"
)

type MailSuite struct {
	BaseSuite

	l         net.Listener
	server    *smtp.Server
	smtpdHost string
	smtpdPort int
	fromCh    chan string
	dataCh    chan string // Channel to receive email body data for attachment verification
}

var _ = Suite(&MailSuite{})

func (s *MailSuite) SetUpTest(c *C) {
	s.BaseSuite.SetUpTest(c)

	s.fromCh = make(chan string, 1)
	s.dataCh = make(chan string, 1)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, IsNil)

	s.l = ln
	// Initialize server outside of the goroutine to avoid racy field writes
	fromCh := s.fromCh
	dataCh := s.dataCh
	srv := smtp.NewServer(&testBackend{fromCh: fromCh, dataCh: dataCh})
	srv.AllowInsecureAuth = true
	s.server = srv
	go func(srv *smtp.Server, ln net.Listener) {
		// Serve on the pre-bound listener
		err := srv.Serve(ln)
		// Only assert if it's not the expected listener close during teardown
		if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			c.Assert(err, IsNil)
		}
	}(srv, ln)

	p := strings.Split(s.l.Addr().String(), ":")
	s.smtpdHost = p[0]
	s.smtpdPort, _ = strconv.Atoi(p[1])
}

func (s *MailSuite) TearDownTest(c *C) {
	s.l.Close()
}

func (s *MailSuite) TestNewSlackEmpty(c *C) {
	c.Assert(NewMail(&MailConfig{}), IsNil)
}

func (s *MailSuite) TestRunSuccess(c *C) {
	s.ctx.Start()
	s.ctx.Stop(nil)

	m := NewMail(&MailConfig{
		SMTPHost:  s.smtpdHost,
		SMTPPort:  s.smtpdPort,
		EmailTo:   "foo@foo.com",
		EmailFrom: "qux@qux.com",
	})

	done := make(chan struct{})
	go func() {
		c.Assert(m.Run(s.ctx), IsNil)
		close(done)
	}()

	select {
	case from := <-s.fromCh:
		c.Assert(from, Equals, "qux@qux.com")
	case <-time.After(3 * time.Second):
		c.Errorf("timeout waiting for SMTP server to receive MAIL FROM")
	}

	<-done
}

// TestRunWithEmptyStreams verifies that zero-sized attachments are not sent
// when stdout/stderr streams are empty (fixes issue #326 - SMTP servers like
// Postmark reject zero-sized attachments).
func (s *MailSuite) TestRunWithEmptyStreams(c *C) {
	s.ctx.Start()
	s.ctx.Stop(nil)

	// Ensure streams are empty (they should be by default)
	c.Assert(s.ctx.Execution.OutputStream.TotalWritten(), Equals, int64(0))
	c.Assert(s.ctx.Execution.ErrorStream.TotalWritten(), Equals, int64(0))

	m := NewMail(&MailConfig{
		SMTPHost:  s.smtpdHost,
		SMTPPort:  s.smtpdPort,
		EmailTo:   "foo@foo.com",
		EmailFrom: "qux@qux.com",
	})

	done := make(chan struct{})
	go func() {
		c.Assert(m.Run(s.ctx), IsNil)
		close(done)
	}()

	select {
	case from := <-s.fromCh:
		c.Assert(from, Equals, "qux@qux.com")
	case <-time.After(3 * time.Second):
		c.Errorf("timeout waiting for SMTP server to receive MAIL FROM")
	}

	// Verify that stdout/stderr attachments are NOT included when streams are empty
	select {
	case emailData := <-s.dataCh:
		// Email should NOT contain stdout.log or stderr.log attachments
		c.Assert(strings.Contains(emailData, "stdout.log"), Equals, false,
			Commentf("stdout.log attachment should not be included for empty streams"))
		c.Assert(strings.Contains(emailData, "stderr.log"), Equals, false,
			Commentf("stderr.log attachment should not be included for empty streams"))
		// But should still contain the JSON attachment with job metadata
		c.Assert(strings.Contains(emailData, ".json"), Equals, true,
			Commentf("JSON attachment with job metadata should always be included"))
	case <-time.After(3 * time.Second):
		c.Errorf("timeout waiting for email data")
	}

	<-done
}

// TestRunWithNonEmptyStreams verifies that attachments are sent when streams have content.
func (s *MailSuite) TestRunWithNonEmptyStreams(c *C) {
	s.ctx.Start()
	// Write some output to streams
	_, _ = s.ctx.Execution.OutputStream.Write([]byte("stdout content"))
	_, _ = s.ctx.Execution.ErrorStream.Write([]byte("stderr content"))
	s.ctx.Stop(nil)

	c.Assert(s.ctx.Execution.OutputStream.TotalWritten() > 0, Equals, true)
	c.Assert(s.ctx.Execution.ErrorStream.TotalWritten() > 0, Equals, true)

	m := NewMail(&MailConfig{
		SMTPHost:  s.smtpdHost,
		SMTPPort:  s.smtpdPort,
		EmailTo:   "foo@foo.com",
		EmailFrom: "qux@qux.com",
	})

	done := make(chan struct{})
	go func() {
		c.Assert(m.Run(s.ctx), IsNil)
		close(done)
	}()

	select {
	case from := <-s.fromCh:
		c.Assert(from, Equals, "qux@qux.com")
	case <-time.After(3 * time.Second):
		c.Errorf("timeout waiting for SMTP server to receive MAIL FROM")
	}

	// Verify that stdout/stderr attachments ARE included when streams have content
	select {
	case emailData := <-s.dataCh:
		// Email should contain stdout.log and stderr.log attachments
		c.Assert(strings.Contains(emailData, "stdout.log"), Equals, true,
			Commentf("stdout.log attachment should be included for non-empty streams"))
		c.Assert(strings.Contains(emailData, "stderr.log"), Equals, true,
			Commentf("stderr.log attachment should be included for non-empty streams"))
		// Should also contain the JSON attachment with job metadata
		c.Assert(strings.Contains(emailData, ".json"), Equals, true,
			Commentf("JSON attachment with job metadata should always be included"))
	case <-time.After(3 * time.Second):
		c.Errorf("timeout waiting for email data")
	}

	<-done
}

// test SMTP backend using github.com/emersion/go-smtp
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
	// Capture email body for attachment verification
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if s.dataCh != nil {
		s.dataCh <- buf.String()
	}
	return nil
}

func (s *testSession) Reset()        {}
func (s *testSession) Logout() error { return nil }
