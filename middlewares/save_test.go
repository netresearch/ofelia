package middlewares

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

const testNameFoo = "foo"

func setupSaveTestContext(t *testing.T) (*core.Context, *TestJob) {
	t.Helper()
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

	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	require.NoError(t, err)

	ctx := core.NewContext(sh, job, e)
	return ctx, &job.TestJob
}

func TestNewSaveEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, NewSave(&SaveConfig{}))
}

func TestSaveRunSuccess(t *testing.T) {
	t.Parallel()
	ctx, job := setupSaveTestContext(t)

	dir := t.TempDir()

	ctx.Start()
	ctx.Stop(nil)

	job.Name = testNameFoo
	ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	require.NoError(t, m.Run(ctx))

	_, err := os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".json"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".stdout.log"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".stderr.log"))
	require.NoError(t, err)
}

func TestSaveRunSuccessOnError(t *testing.T) {
	t.Parallel()
	ctx, job := setupSaveTestContext(t)

	dir := t.TempDir()

	ctx.Start()
	ctx.Stop(nil)

	job.Name = testNameFoo
	ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir, SaveOnlyOnError: new(true)})
	require.NoError(t, m.Run(ctx))

	_, err := os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".json"))
	assert.Error(t, err)
}

func TestSaveSensitiveData(t *testing.T) {
	t.Parallel()
	ctx, job := setupSaveTestContext(t)

	dir := t.TempDir()

	ctx.Start()
	ctx.Stop(nil)

	job.Name = "job-with-sensitive-data"
	ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	require.NoError(t, m.Run(ctx))

	expectedFileName := "00010101_000000_job-with-sensitive-data"
	_, err := os.Stat(filepath.Join(dir, expectedFileName+".json"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, expectedFileName+".stdout.log"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, expectedFileName+".stderr.log"))
	require.NoError(t, err)

	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, files, 3)

	for _, file := range files {
		b, err := os.ReadFile(filepath.Join(dir, file.Name()))
		require.NoError(t, err)

		if strings.Contains(string(b), "secret") {
			t.Logf("Content: %s", string(b))
			t.Errorf("found secret string in %q", file.Name())
		}
	}
}

func TestSaveCreatesSaveFolder(t *testing.T) {
	t.Parallel()
	ctx, job := setupSaveTestContext(t)

	dir := filepath.Join(t.TempDir(), "save-subdir")

	ctx.Start()
	ctx.Stop(nil)

	job.Name = testNameFoo
	ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	require.NoError(t, m.Run(ctx))

	fi, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
}

func TestSaveSafeFilename(t *testing.T) {
	t.Parallel()
	ctx, job := setupSaveTestContext(t)

	dir := t.TempDir()

	ctx.Start()
	ctx.Stop(nil)

	job.Name = "foo/bar\\baz"
	ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	require.NoError(t, m.Run(ctx))

	safe := strings.NewReplacer("/", "_", "\\", "_").Replace(job.Name)
	_, err := os.Stat(filepath.Join(dir, "00010101_000000_"+safe+".stdout.log"))
	require.NoError(t, err)
}
