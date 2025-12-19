package cli

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateExecuteValidFile(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp("", "ofelia_valid_*.ini")
	require.NoError(t, err)
	defer os.Remove(file.Name())

	content := `
[job-exec "foo"]
schedule = @every 10s
command = echo "foo"
`
	_, err = file.WriteString(content)
	require.NoError(t, err)
	file.Close()

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	cmd := ValidateCommand{ConfigFile: file.Name(), Logger: &TestLogger{}}
	err = cmd.Execute(nil)
	require.NoError(t, err)

	w.Close()
	out, _ := io.ReadAll(r)

	var conf Config
	err = json.Unmarshal(out, &conf)
	require.NoError(t, err)
	job, ok := conf.ExecJobs["foo"]
	assert.True(t, ok)
	assert.Equal(t, 10, job.HistoryLimit)
}

func TestValidateExecuteInvalidFile(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp("", "ofelia_invalid_*.ini")
	require.NoError(t, err)
	defer os.Remove(file.Name())

	_, err = file.WriteString("[job-exec \"foo\"\nschedule = @every 10s\n")
	require.NoError(t, err)
	file.Close()

	cmd := ValidateCommand{ConfigFile: file.Name(), Logger: &TestLogger{}}
	err = cmd.Execute(nil)
	assert.Error(t, err)
}

func TestValidateExecuteMissingFile(t *testing.T) {
	t.Parallel()

	cmd := ValidateCommand{ConfigFile: "/nonexistent/ofelia/config.ini", Logger: &TestLogger{}}
	err := cmd.Execute(nil)
	assert.Error(t, err)
}
