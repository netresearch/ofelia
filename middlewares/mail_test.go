package middlewares

import (
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
}

var _ = Suite(&MailSuite{})

func (s *MailSuite) SetUpTest(c *C) {
	s.BaseSuite.SetUpTest(c)

	s.fromCh = make(chan string, 1)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, IsNil)

	s.l = ln
	// Initialize server outside of the goroutine to avoid racy field writes
	fromCh := s.fromCh
	srv := smtp.NewServer(&testBackend{fromCh: fromCh})
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

// test SMTP backend using github.com/emersion/go-smtp
type testBackend struct {
	fromCh chan string
}

func (b *testBackend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &testSession{fromCh: b.fromCh}, nil
}

type testSession struct {
	fromCh chan string
}

func (s *testSession) Mail(from string, _ *smtp.MailOptions) error {
	s.fromCh <- from
	return nil
}

func (s *testSession) Rcpt(_ string, _ *smtp.RcptOptions) error { return nil }

func (s *testSession) Data(r io.Reader) error {
	// Drain data
	_, _ = io.Copy(io.Discard, r)
	return nil
}

func (s *testSession) Reset()        {}
func (s *testSession) Logout() error { return nil }
