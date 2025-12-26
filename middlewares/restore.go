package middlewares

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/netresearch/ofelia/core"
)

// savedExecution represents the JSON structure saved by the Save middleware.
// The JSON field names match the Save middleware's output format (PascalCase).
//
//nolint:tagliatelle // JSON format is defined by the Save middleware, must match exactly
type savedExecution struct {
	Job struct {
		Name string `json:"Name"`
	} `json:"Job"`
	Execution struct {
		ID        string        `json:"ID"`
		Date      time.Time     `json:"Date"`
		Duration  time.Duration `json:"Duration"`
		IsRunning bool          `json:"IsRunning"`
		Failed    bool          `json:"Failed"`
		Skipped   bool          `json:"Skipped"`
	} `json:"Execution"`
}

// restoredEntry holds a parsed execution ready for restoration.
type restoredEntry struct {
	JobName   string
	Execution *core.Execution
}

// RestoreHistory restores job history from saved JSON files in the save folder.
// It populates the in-memory history of jobs that support SetLastRun.
// Only files newer than maxAge are restored.
func RestoreHistory(saveFolder string, maxAge time.Duration, jobs []core.Job, logger core.Logger) error {
	if saveFolder == "" {
		return nil
	}

	// Validate save folder - skip silently if invalid
	if DefaultSanitizer.ValidateSaveFolder(saveFolder) != nil {
		return nil //nolint:nilerr // Intentionally skip invalid folders
	}

	// Check if folder exists
	info, err := os.Stat(saveFolder)
	if os.IsNotExist(err) {
		logger.Debugf("Save folder %q does not exist, skipping history restoration", saveFolder)
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat save folder: %w", err)
	}
	if !info.IsDir() {
		return nil
	}

	// Build job lookup map
	jobsByName := make(map[string]core.Job)
	for _, job := range jobs {
		jobsByName[job.GetName()] = job
	}

	// Find and parse JSON files
	cutoff := time.Now().Add(-maxAge)
	entries, err := parseHistoryFiles(saveFolder, cutoff, logger)
	if err != nil {
		logger.Warningf("Error scanning save folder: %v", err)
		return nil // Don't fail startup for restore errors
	}

	if len(entries) == 0 {
		logger.Debugf("No history files found to restore")
		return nil
	}

	// Group entries by job name and sort by date
	entriesByJob := make(map[string][]*restoredEntry)
	for _, entry := range entries {
		entriesByJob[entry.JobName] = append(entriesByJob[entry.JobName], entry)
	}

	restoredCount := 0
	for jobName, jobEntries := range entriesByJob {
		job, exists := jobsByName[jobName]
		if !exists {
			logger.Debugf("Skipping history for unknown job %q", jobName)
			continue
		}

		// Sort by date ascending (oldest first) so SetLastRun works correctly
		sort.Slice(jobEntries, func(i, j int) bool {
			return jobEntries[i].Execution.Date.Before(jobEntries[j].Execution.Date)
		})

		// Check if job supports SetLastRun
		setter, ok := job.(interface{ SetLastRun(*core.Execution) })
		if !ok {
			logger.Debugf("Job %q does not support history restoration", jobName)
			continue
		}

		// Restore each execution
		for _, entry := range jobEntries {
			setter.SetLastRun(entry.Execution)
			restoredCount++
		}
	}

	if restoredCount > 0 {
		logger.Noticef("Restored %d history entries for %d job(s) from saved files", restoredCount, len(entriesByJob))
	}

	return nil
}

// parseHistoryFiles scans the save folder for JSON files and parses them.
func parseHistoryFiles(saveFolder string, cutoff time.Time, logger core.Logger) ([]*restoredEntry, error) {
	var entries []*restoredEntry

	err := filepath.Walk(saveFolder, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // Intentionally skip inaccessible files
		}

		// Only process .json files
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		// Skip files older than cutoff based on modification time (quick filter)
		if info.ModTime().Before(cutoff) {
			return nil
		}

		// Parse the JSON file
		entry, err := parseHistoryFile(path)
		if err != nil {
			logger.Debugf("Skipping invalid history file %q: %v", path, err)
			return nil
		}

		// Skip if execution date is too old
		if entry.Execution.Date.Before(cutoff) {
			return nil
		}

		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return entries, fmt.Errorf("walk save folder: %w", err)
	}
	return entries, nil
}

// parseHistoryFile reads and parses a single JSON history file.
func parseHistoryFile(path string) (*restoredEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var saved savedExecution
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}

	// Validate required fields
	if saved.Job.Name == "" {
		return nil, os.ErrInvalid
	}

	// Convert to core.Execution
	exec := &core.Execution{
		ID:        saved.Execution.ID,
		Date:      saved.Execution.Date,
		Duration:  saved.Execution.Duration,
		IsRunning: false, // Never restore as running
		Failed:    saved.Execution.Failed,
		Skipped:   saved.Execution.Skipped,
	}

	return &restoredEntry{
		JobName:   saved.Job.Name,
		Execution: exec,
	}, nil
}
