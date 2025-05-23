package cli

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func TestValidate(t *testing.T) { TestingT(t) }

// ValidateSuite is the test suite for ValidateCommand.
type ValidateSuite struct{}

var _ = Suite(&ValidateSuite{})

// TestExecuteValidFile verifies that Execute returns no error for a valid config file.
func (s *ValidateSuite) TestExecuteValidFile(c *C) {
	// Create a temporary INI file with a valid job entry.
	file, err := os.CreateTemp("", "ofelia_valid_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(file.Name())

	content := `
[job-exec "foo"]
schedule = @every 10s
command = echo "foo"
`
	_, err = file.WriteString(content)
	c.Assert(err, IsNil)
	file.Close()

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	cmd := ValidateCommand{ConfigFile: file.Name(), Logger: &TestLogger{}}
	err = cmd.Execute(nil)
	c.Assert(err, IsNil)

	w.Close()
	out, _ := io.ReadAll(r)

	var conf Config
	err = json.Unmarshal(out, &conf)
	c.Assert(err, IsNil)
	job, ok := conf.ExecJobs["foo"]
	c.Assert(ok, Equals, true)
	c.Assert(job.HistoryLimit, Equals, 10)
}

// TestExecuteInvalidFile verifies that Execute returns an error for malformed config file.
func (s *ValidateSuite) TestExecuteInvalidFile(c *C) {
	// Create a temporary INI file with invalid syntax.
	file, err := os.CreateTemp("", "ofelia_invalid_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(file.Name())

	// Missing closing bracket in section header
	_, err = file.WriteString("[job-exec \"foo\"\nschedule = @every 10s\n")
	c.Assert(err, IsNil)
	file.Close()

	cmd := ValidateCommand{ConfigFile: file.Name(), Logger: &TestLogger{}}
	err = cmd.Execute(nil)
	c.Assert(err, NotNil)
}

// TestExecuteMissingFile verifies that Execute returns an error when file does not exist.
func (s *ValidateSuite) TestExecuteMissingFile(c *C) {
	cmd := ValidateCommand{ConfigFile: "/nonexistent/ofelia.conf", Logger: &TestLogger{}}
	err := cmd.Execute(nil)
	c.Assert(err, NotNil)
}
