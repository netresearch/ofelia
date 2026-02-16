package middlewares

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// setupTestContext creates a fresh test context for middleware tests
func setupTestContext(t *testing.T) (*core.Context, *TestJob) {
	t.Helper()
	job := &TestJob{}
	sh := core.NewScheduler(newDiscardLogger())
	e, err := core.NewExecution()
	require.NoError(t, err)
	return core.NewContext(sh, job, e), job
}

func TestIsEmpty(t *testing.T) {
	t.Parallel()

	config := &TestConfig{}
	assert.True(t, IsEmpty(config))

	config = &TestConfig{Foo: "foo"}
	assert.False(t, IsEmpty(config))

	config = &TestConfig{Qux: 42}
	assert.False(t, IsEmpty(config))
}

type TestConfig struct {
	Foo string
	Qux int
	Bar bool
}

type TestJob struct {
	core.BareJob
}

type TestJobConfig struct {
	TestJob
	MailConfig
	OverlapConfig
	SaveConfig
	SlackConfig
}

func (j *TestJob) Run(ctx *core.Context) error {
	return nil
}
