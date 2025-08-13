package middlewares

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/netresearch/ofelia/core"
)

const testNameFoo = "foo"

type SuiteSave struct {
	BaseSuite
}

var _ = Suite(&SuiteSave{})

func (s *SuiteSave) SetUpTest(c *C) {
	job := &TestJobConfig{
		TestJob: TestJob{
			BareJob: core.BareJob{
				Name: "test-job-save",
			},
		},
		MailConfig: MailConfig{
			SMTPHost:     "test-host",
			SMTPPassword: "secret-password",
			SMTPUser:     "secret-user",
		},
		SlackConfig: SlackConfig{
			SlackWebhook: "secret-url",
		},
	}

	s.job = &job.TestJob

	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	c.Assert(err, IsNil)

	s.ctx = core.NewContext(sh, job, e)
}

func (s *SuiteSave) TestNewSlackEmpty(c *C) {
	c.Assert(NewSave(&SaveConfig{}), IsNil)
}

func (s *SuiteSave) TestRunSuccess(c *C) {
	dir, err := os.MkdirTemp("/tmp", "save")
	c.Assert(err, IsNil)
	defer os.RemoveAll(dir)

	s.ctx.Start()
	s.ctx.Stop(nil)

	s.job.Name = testNameFoo
	s.ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	c.Assert(m.Run(s.ctx), IsNil)

	_, err = os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".json"))
	c.Assert(err, IsNil)

	_, err = os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".stdout.log"))
	c.Assert(err, IsNil)

	_, err = os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".stderr.log"))
	c.Assert(err, IsNil)
}

func (s *SuiteSave) TestRunSuccessOnError(c *C) {
	dir, err := os.MkdirTemp("/tmp", "save")
	c.Assert(err, IsNil)
	defer os.RemoveAll(dir)

	s.ctx.Start()
	s.ctx.Stop(nil)

	s.job.Name = testNameFoo
	s.ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir, SaveOnlyOnError: true})
	c.Assert(m.Run(s.ctx), IsNil)

	_, err = os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".json"))
	c.Assert(err, Not(IsNil))
}

func (s *SuiteSave) TestSensitiveData(c *C) {
	dir, err := os.MkdirTemp("/tmp", "save")
	c.Assert(err, IsNil)
	defer os.RemoveAll(dir)

	s.ctx.Start()
	s.ctx.Stop(nil)

	s.job.Name = "job-with-sensitive-data"
	s.ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	c.Assert(m.Run(s.ctx), IsNil)

	expectedFileName := "00010101_000000_job-with-sensitive-data"
	_, err = os.Stat(filepath.Join(dir, expectedFileName+".json"))
	c.Assert(err, IsNil)

	_, err = os.Stat(filepath.Join(dir, expectedFileName+".stdout.log"))
	c.Assert(err, IsNil)

	_, err = os.Stat(filepath.Join(dir, expectedFileName+".stderr.log"))
	c.Assert(err, IsNil)

	files, err := os.ReadDir(dir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 3)

	for _, file := range files {
		b, err := os.ReadFile(filepath.Join(dir, file.Name()))
		c.Assert(err, IsNil)

		if strings.Contains(string(b), "secret") {
			c.Log(string(b))
			c.Errorf("found secret string in %q", file.Name())
		}
	}
}

func (s *SuiteSave) TestCreatesSaveFolder(c *C) {
	dir, err := os.MkdirTemp("/tmp", "save")
	c.Assert(err, IsNil)
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	s.ctx.Start()
	s.ctx.Stop(nil)

	s.job.Name = testNameFoo
	s.ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	c.Assert(m.Run(s.ctx), IsNil)

	fi, err := os.Stat(dir)
	c.Assert(err, IsNil)
	c.Assert(fi.IsDir(), Equals, true)
}

func (s *SuiteSave) TestSafeFilename(c *C) {
	dir, err := os.MkdirTemp("/tmp", "save")
	c.Assert(err, IsNil)
	defer os.RemoveAll(dir)

	s.ctx.Start()
	s.ctx.Stop(nil)

	s.job.Name = "foo/bar\\baz"
	s.ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	c.Assert(m.Run(s.ctx), IsNil)

	safe := strings.NewReplacer("/", "_", "\\", "_").Replace(s.job.Name)
	_, err = os.Stat(filepath.Join(dir, "00010101_000000_"+safe+".stdout.log"))
	c.Assert(err, IsNil)
}
