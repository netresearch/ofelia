package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/netresearch/ofelia/test"
)

func TestMergeJobMaps(t *testing.T) {
	t.Parallel()

	t.Run("left empty right has keys", func(t *testing.T) {
		t.Parallel()
		right := map[string]string{"a": "right"}
		got := mergeJobMaps(nil, right, true)
		assert.Equal(t, right, got)
		got = mergeJobMaps(nil, right, false)
		assert.Equal(t, right, got)
	})

	t.Run("same key useRightIfExists false keeps left", func(t *testing.T) {
		t.Parallel()
		left := map[string]string{"a": "left"}
		right := map[string]string{"a": "right"}
		got := mergeJobMaps(left, right, false)
		assert.Equal(t, "left", got["a"])
	})

	t.Run("same key useRightIfExists true uses right", func(t *testing.T) {
		t.Parallel()
		left := map[string]string{"a": "left"}
		right := map[string]string{"a": "right"}
		got := mergeJobMaps(left, right, true)
		assert.Equal(t, "right", got["a"])
	})

	t.Run("disjoint keys both present", func(t *testing.T) {
		t.Parallel()
		left := map[string]string{"a": "left"}
		right := map[string]string{"b": "right"}
		got := mergeJobMaps(left, right, false)
		assert.Len(t, got, 2)
		assert.Equal(t, "left", got["a"])
		assert.Equal(t, "right", got["b"])
		got = mergeJobMaps(left, right, true)
		assert.Len(t, got, 2)
		assert.Equal(t, "left", got["a"])
		assert.Equal(t, "right", got["b"])
	})

	t.Run("both empty", func(t *testing.T) {
		t.Parallel()
		got := mergeJobMaps(map[string]string{}, map[string]string{}, true)
		assert.NotNil(t, got)
		assert.Len(t, got, 0)
	})
}

func TestCanRunServiceJob(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("job-local on non-service returns false", func(t *testing.T) {
		t.Parallel()
		got := canRunServiceJob(jobLocal, "myjob", "c1", false, logger)
		assert.False(t, got)
	})

	t.Run("job-service-run on non-service returns false", func(t *testing.T) {
		t.Parallel()
		got := canRunServiceJob(jobServiceRun, "myjob", "c1", false, logger)
		assert.False(t, got)
	})

	t.Run("job-local on service returns true", func(t *testing.T) {
		t.Parallel()
		got := canRunServiceJob(jobLocal, "myjob", "c1", true, logger)
		assert.True(t, got)
	})

	t.Run("job-exec on non-service returns true", func(t *testing.T) {
		t.Parallel()
		got := canRunServiceJob(jobExec, "myjob", "c1", false, logger)
		assert.True(t, got)
	})

	t.Run("job-run on non-service returns true", func(t *testing.T) {
		t.Parallel()
		got := canRunServiceJob(jobRun, "myjob", "c1", false, logger)
		assert.True(t, got)
	})

	t.Run("job-compose on non-service returns true", func(t *testing.T) {
		t.Parallel()
		got := canRunServiceJob(jobCompose, "myjob", "c1", false, logger)
		assert.True(t, got)
	})

	t.Run("unknown job type returns false", func(t *testing.T) {
		t.Parallel()
		got := canRunServiceJob("job-unknown", "myjob", "c1", false, logger)
		assert.False(t, got)
	})
}

func TestCanRunJobInStoppedContainer(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("job-exec on stopped returns false", func(t *testing.T) {
		t.Parallel()
		got := canRunJobInStoppedContainer(jobExec, "myjob", "c1", false, logger)
		assert.False(t, got)
	})

	t.Run("job-exec on running returns true", func(t *testing.T) {
		t.Parallel()
		got := canRunJobInStoppedContainer(jobExec, "myjob", "c1", true, logger)
		assert.True(t, got)
	})

	t.Run("job-run on stopped returns true", func(t *testing.T) {
		t.Parallel()
		got := canRunJobInStoppedContainer(jobRun, "myjob", "c1", false, logger)
		assert.True(t, got)
	})

	t.Run("job-run on running returns true", func(t *testing.T) {
		t.Parallel()
		got := canRunJobInStoppedContainer(jobRun, "myjob", "c1", true, logger)
		assert.True(t, got)
	})

	t.Run("unknown job type returns false", func(t *testing.T) {
		t.Parallel()
		got := canRunJobInStoppedContainer("job-unknown", "myjob", "c1", false, logger)
		assert.False(t, got)
	})
}
