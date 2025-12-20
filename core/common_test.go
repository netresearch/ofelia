package core

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type hashJob struct {
	Str string `hash:"true"`
	Num int    `hash:"true"`
	Flg bool   `hash:"true"`
}

func TestGetHash_SupportedKinds(t *testing.T) {
	t.Parallel()

	var h string
	val := &hashJob{Str: "x", Num: 7, Flg: true}
	err := GetHash(reflect.TypeOf(val).Elem(), reflect.ValueOf(val).Elem(), &h)
	require.NoError(t, err)
	assert.NotEmpty(t, h)
}

func TestExecutionStopFlagsAndDuration(t *testing.T) {
	t.Parallel()

	e := &Execution{}
	start := time.Now()
	e.Date = start
	e.Start()
	e.Stop(ErrSkippedExecution)

	assert.True(t, e.Skipped)
	assert.False(t, e.Failed)
	assert.Greater(t, e.Duration, time.Duration(0))

	e = &Execution{}
	e.Start()
	e.Stop(assertError{})

	require.Error(t, e.Error)
	assert.True(t, e.Failed)
}

type assertError struct{}

func (assertError) Error() string { return "boom" }

func TestBareJobHistory(t *testing.T) {
	t.Parallel()

	j := &BareJob{HistoryLimit: 2}
	for i := range 3 {
		_ = i
		e := &Execution{}
		j.SetLastRun(e)
	}

	assert.NotNil(t, j.GetLastRun())
	assert.Len(t, j.GetHistory(), 2)
}

func TestExecutionGetStdout(t *testing.T) {
	t.Parallel()

	e, err := NewExecution()
	require.NoError(t, err)

	testOutput := "test stdout content"
	_, err = e.OutputStream.Write([]byte(testOutput))
	require.NoError(t, err)

	assert.Equal(t, testOutput, e.GetStdout())

	e.Cleanup()
	assert.Equal(t, testOutput, e.GetStdout())
	assert.Nil(t, e.OutputStream)
	assert.Equal(t, testOutput, e.CapturedStdout)
}

func TestExecutionGetStderr(t *testing.T) {
	t.Parallel()

	e, err := NewExecution()
	require.NoError(t, err)

	testError := "test stderr content"
	_, err = e.ErrorStream.Write([]byte(testError))
	require.NoError(t, err)

	assert.Equal(t, testError, e.GetStderr())

	e.Cleanup()
	assert.Equal(t, testError, e.GetStderr())
	assert.Nil(t, e.ErrorStream)
	assert.Equal(t, testError, e.CapturedStderr)
}

func TestExecutionOutputCleanup(t *testing.T) {
	t.Parallel()

	e, err := NewExecution()
	require.NoError(t, err)

	stdoutContent := "stdout test"
	stderrContent := "stderr test"

	_, err = e.OutputStream.Write([]byte(stdoutContent))
	require.NoError(t, err)

	_, err = e.ErrorStream.Write([]byte(stderrContent))
	require.NoError(t, err)

	assert.NotNil(t, e.OutputStream)
	assert.NotNil(t, e.ErrorStream)

	e.Cleanup()

	assert.Nil(t, e.OutputStream)
	assert.Nil(t, e.ErrorStream)
	assert.Equal(t, stdoutContent, e.CapturedStdout)
	assert.Equal(t, stderrContent, e.CapturedStderr)
	assert.Equal(t, stdoutContent, e.GetStdout())
	assert.Equal(t, stderrContent, e.GetStderr())
}

func TestExecutionEmptyOutput(t *testing.T) {
	t.Parallel()

	e, err := NewExecution()
	require.NoError(t, err)

	assert.Empty(t, e.GetStdout())
	assert.Empty(t, e.GetStderr())

	e.Cleanup()
	assert.Empty(t, e.GetStdout())
	assert.Empty(t, e.GetStderr())
}
