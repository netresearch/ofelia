package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateExecuteValidFile(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.ini")
	content := `
[job-exec "foo"]
schedule = @every 10s
command = echo "foo"
`
	err := os.WriteFile(configFile, []byte(content), 0o644)
	require.NoError(t, err)

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	cmd := ValidateCommand{ConfigFile: configFile, Logger: &TestLogger{}}
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

	configFile := filepath.Join(t.TempDir(), "config.ini")
	err := os.WriteFile(configFile, []byte("[job-exec \"foo\"\nschedule = @every 10s\n"), 0o644)
	require.NoError(t, err)

	cmd := ValidateCommand{ConfigFile: configFile, Logger: &TestLogger{}}
	err = cmd.Execute(nil)
	assert.Error(t, err)
}

func TestValidateExecuteMissingFile(t *testing.T) {
	t.Parallel()

	cmd := ValidateCommand{ConfigFile: "/nonexistent/ofelia/config.ini", Logger: &TestLogger{}}
	err := cmd.Execute(nil)
	assert.Error(t, err)
}
