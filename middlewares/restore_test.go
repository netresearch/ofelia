package middlewares

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

// TestLogger is defined in common_test.go

func TestRestoreHistory_EmptyFolder(t *testing.T) {
	dir := t.TempDir()
	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}

	err := RestoreHistory(dir, 24*time.Hour, []core.Job{job}, &TestLogger{})
	require.NoError(t, err)
	assert.Empty(t, job.GetHistory())
}

func TestRestoreHistory_NoFolder(t *testing.T) {
	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}

	err := RestoreHistory("", 24*time.Hour, []core.Job{job}, &TestLogger{})
	assert.NoError(t, err)
}

func TestRestoreHistory_NonExistentFolder(t *testing.T) {
	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}

	err := RestoreHistory("/nonexistent/folder/path", 24*time.Hour, []core.Job{job}, &TestLogger{})
	assert.NoError(t, err)
}

func TestRestoreHistory_SingleExecution(t *testing.T) {
	dir := t.TempDir()
	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}

	// Create a saved execution JSON file
	execTime := time.Now().Add(-1 * time.Hour)
	savedData := map[string]any{
		"Job": map[string]any{
			"Name": "test-job",
		},
		"Execution": map[string]any{
			"ID":       "exec-1",
			"Date":     execTime,
			"Duration": float64(5 * time.Second),
			"Failed":   false,
			"Skipped":  false,
		},
	}

	jsonData, err := json.MarshalIndent(savedData, "", "  ")
	require.NoError(t, err)

	filename := filepath.Join(dir, execTime.Format("20060102_150405")+"_test-job.json")
	err = os.WriteFile(filename, jsonData, 0o600)
	require.NoError(t, err)

	err = RestoreHistory(dir, 24*time.Hour, []core.Job{job}, &TestLogger{})
	require.NoError(t, err)

	history := job.GetHistory()
	require.Len(t, history, 1)
	assert.Equal(t, "exec-1", history[0].ID)
	assert.False(t, history[0].Failed)
	assert.False(t, history[0].IsRunning)
}

func TestRestoreHistory_MultipleExecutions(t *testing.T) {
	dir := t.TempDir()
	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}

	// Create multiple saved execution JSON files
	for i := range 3 {
		execTime := time.Now().Add(-time.Duration(3-i) * time.Hour)
		savedData := map[string]any{
			"Job": map[string]any{
				"Name": "test-job",
			},
			"Execution": map[string]any{
				"ID":       "exec-" + string(rune('1'+i)),
				"Date":     execTime,
				"Duration": float64(5 * time.Second),
				"Failed":   i == 1, // Middle one failed
				"Skipped":  false,
			},
		}

		jsonData, err := json.MarshalIndent(savedData, "", "  ")
		require.NoError(t, err)

		filename := filepath.Join(dir, execTime.Format("20060102_150405")+"_test-job.json")
		err = os.WriteFile(filename, jsonData, 0o600)
		require.NoError(t, err)
	}

	err := RestoreHistory(dir, 24*time.Hour, []core.Job{job}, &TestLogger{})
	require.NoError(t, err)

	history := job.GetHistory()
	require.Len(t, history, 3)
	// History should be sorted by date (oldest first due to SetLastRun behavior)
	assert.True(t, history[0].Date.Before(history[1].Date))
	assert.True(t, history[1].Date.Before(history[2].Date))
}

func TestRestoreHistory_RespectsMaxAge(t *testing.T) {
	dir := t.TempDir()
	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}

	// Create an old execution (48 hours ago)
	oldExecTime := time.Now().Add(-48 * time.Hour)
	oldData := map[string]any{
		"Job": map[string]any{
			"Name": "test-job",
		},
		"Execution": map[string]any{
			"ID":       "old-exec",
			"Date":     oldExecTime,
			"Duration": float64(5 * time.Second),
			"Failed":   false,
			"Skipped":  false,
		},
	}
	oldJson, _ := json.MarshalIndent(oldData, "", "  ")
	oldFilename := filepath.Join(dir, oldExecTime.Format("20060102_150405")+"_test-job.json")
	err := os.WriteFile(oldFilename, oldJson, 0o600)
	require.NoError(t, err)
	// Set file modification time to match
	err = os.Chtimes(oldFilename, oldExecTime, oldExecTime)
	require.NoError(t, err)

	// Create a recent execution (1 hour ago)
	recentExecTime := time.Now().Add(-1 * time.Hour)
	recentData := map[string]any{
		"Job": map[string]any{
			"Name": "test-job",
		},
		"Execution": map[string]any{
			"ID":       "recent-exec",
			"Date":     recentExecTime,
			"Duration": float64(5 * time.Second),
			"Failed":   false,
			"Skipped":  false,
		},
	}
	recentJson, _ := json.MarshalIndent(recentData, "", "  ")
	recentFilename := filepath.Join(dir, recentExecTime.Format("20060102_150405")+"_test-job.json")
	err = os.WriteFile(recentFilename, recentJson, 0o600)
	require.NoError(t, err)

	// Restore with 24h max age - should only get recent execution
	err = RestoreHistory(dir, 24*time.Hour, []core.Job{job}, &TestLogger{})
	require.NoError(t, err)

	history := job.GetHistory()
	require.Len(t, history, 1)
	assert.Equal(t, "recent-exec", history[0].ID)
}

func TestRestoreHistory_SkipsUnknownJobs(t *testing.T) {
	dir := t.TempDir()
	job := &core.BareJob{Name: "known-job", HistoryLimit: 10}

	// Create execution for unknown job
	execTime := time.Now().Add(-1 * time.Hour)
	savedData := map[string]any{
		"Job": map[string]any{
			"Name": "unknown-job",
		},
		"Execution": map[string]any{
			"ID":       "exec-1",
			"Date":     execTime,
			"Duration": float64(5 * time.Second),
			"Failed":   false,
			"Skipped":  false,
		},
	}

	jsonData, _ := json.MarshalIndent(savedData, "", "  ")
	filename := filepath.Join(dir, execTime.Format("20060102_150405")+"_unknown-job.json")
	err := os.WriteFile(filename, jsonData, 0o600)
	require.NoError(t, err)

	err = RestoreHistory(dir, 24*time.Hour, []core.Job{job}, &TestLogger{})
	require.NoError(t, err)
	assert.Empty(t, job.GetHistory())
}

func TestRestoreHistory_SkipsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}

	// Create invalid JSON file
	filename := filepath.Join(dir, "20240101_120000_test-job.json")
	err := os.WriteFile(filename, []byte("not valid json"), 0o600)
	require.NoError(t, err)

	err = RestoreHistory(dir, 24*time.Hour, []core.Job{job}, &TestLogger{})
	require.NoError(t, err)
	assert.Empty(t, job.GetHistory())
}

func TestRestoreHistory_RespectsHistoryLimit(t *testing.T) {
	dir := t.TempDir()
	job := &core.BareJob{Name: "test-job", HistoryLimit: 2}

	// Create 5 executions
	for i := range 5 {
		execTime := time.Now().Add(-time.Duration(5-i) * time.Hour)
		savedData := map[string]any{
			"Job": map[string]any{
				"Name": "test-job",
			},
			"Execution": map[string]any{
				"ID":       "exec-" + string(rune('1'+i)),
				"Date":     execTime,
				"Duration": float64(5 * time.Second),
				"Failed":   false,
				"Skipped":  false,
			},
		}

		jsonData, _ := json.MarshalIndent(savedData, "", "  ")
		filename := filepath.Join(dir, execTime.Format("20060102_150405")+"_test-job.json")
		err := os.WriteFile(filename, jsonData, 0o600)
		require.NoError(t, err)
	}

	err := RestoreHistory(dir, 24*time.Hour, []core.Job{job}, &TestLogger{})
	require.NoError(t, err)

	history := job.GetHistory()
	// Should be limited to 2 (the most recent ones)
	assert.Len(t, history, 2)
}

func TestRestoreHistory_NeverRestoresAsRunning(t *testing.T) {
	dir := t.TempDir()
	job := &core.BareJob{Name: "test-job", HistoryLimit: 10}

	// Create execution that was "running" when saved
	execTime := time.Now().Add(-1 * time.Hour)
	savedData := map[string]any{
		"Job": map[string]any{
			"Name": "test-job",
		},
		"Execution": map[string]any{
			"ID":        "exec-1",
			"Date":      execTime,
			"Duration":  float64(5 * time.Second),
			"IsRunning": true, // Was running when saved
			"Failed":    false,
			"Skipped":   false,
		},
	}

	jsonData, _ := json.MarshalIndent(savedData, "", "  ")
	filename := filepath.Join(dir, execTime.Format("20060102_150405")+"_test-job.json")
	err := os.WriteFile(filename, jsonData, 0o600)
	require.NoError(t, err)

	err = RestoreHistory(dir, 24*time.Hour, []core.Job{job}, &TestLogger{})
	require.NoError(t, err)

	history := job.GetHistory()
	require.Len(t, history, 1)
	assert.False(t, history[0].IsRunning, "Restored execution should never be marked as running")
}

func TestSaveConfig_RestoreHistoryEnabled(t *testing.T) {
	t.Run("defaults to true when save folder is set", func(t *testing.T) {
		cfg := SaveConfig{SaveFolder: "/tmp/logs"}
		assert.True(t, cfg.RestoreHistoryEnabled())
	})

	t.Run("defaults to false when save folder is empty", func(t *testing.T) {
		cfg := SaveConfig{}
		assert.False(t, cfg.RestoreHistoryEnabled())
	})

	t.Run("respects explicit true", func(t *testing.T) {
		enabled := true
		cfg := SaveConfig{RestoreHistory: &enabled}
		assert.True(t, cfg.RestoreHistoryEnabled())
	})

	t.Run("respects explicit false", func(t *testing.T) {
		enabled := false
		cfg := SaveConfig{SaveFolder: "/tmp/logs", RestoreHistory: &enabled}
		assert.False(t, cfg.RestoreHistoryEnabled())
	})
}

func TestSaveConfig_GetRestoreHistoryMaxAge(t *testing.T) {
	t.Run("defaults to 24 hours", func(t *testing.T) {
		cfg := SaveConfig{}
		assert.Equal(t, 24*time.Hour, cfg.GetRestoreHistoryMaxAge())
	})

	t.Run("respects custom value", func(t *testing.T) {
		cfg := SaveConfig{RestoreHistoryMaxAge: 48 * time.Hour}
		assert.Equal(t, 48*time.Hour, cfg.GetRestoreHistoryMaxAge())
	})
}
