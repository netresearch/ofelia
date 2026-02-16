package core

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// WorkflowOrchestrator manages job dependencies and workflow execution
type WorkflowOrchestrator struct {
	scheduler    *Scheduler
	logger       Logger
	dependencies map[string]*DependencyNode
	mu           sync.RWMutex
	executions   map[string]*WorkflowExecution
}

// DependencyNode represents a job in the dependency graph
type DependencyNode struct {
	Job          Job
	Dependencies []string // Job names this job depends on
	Dependents   []string // Job names that depend on this job
	OnSuccess    []string // Jobs to trigger on success
	OnFailure    []string // Jobs to trigger on failure
}

// WorkflowExecution tracks the state of a workflow execution
type WorkflowExecution struct {
	ID            string
	StartTime     time.Time
	CompletedJobs map[string]bool
	FailedJobs    map[string]bool
	RunningJobs   map[string]bool
	mu            sync.RWMutex
}

// NewWorkflowOrchestrator creates a new workflow orchestrator
func NewWorkflowOrchestrator(scheduler *Scheduler, logger Logger) *WorkflowOrchestrator {
	return &WorkflowOrchestrator{
		scheduler:    scheduler,
		logger:       logger,
		dependencies: make(map[string]*DependencyNode),
		executions:   make(map[string]*WorkflowExecution),
	}
}

// BuildDependencyGraph builds the dependency graph from jobs
func (wo *WorkflowOrchestrator) BuildDependencyGraph(jobs []Job) error {
	wo.mu.Lock()
	defer wo.mu.Unlock()

	// Clear existing graph
	wo.dependencies = make(map[string]*DependencyNode)

	// First pass: create nodes
	for _, job := range jobs {
		// Create node with default empty values
		node := &DependencyNode{
			Job:          job,
			Dependencies: []string{},
			OnSuccess:    []string{},
			OnFailure:    []string{},
			Dependents:   []string{},
		}

		// If it's a BareJob, extract dependency configuration
		if bareJob, ok := job.(*BareJob); ok {
			node.Dependencies = bareJob.Dependencies
			node.OnSuccess = bareJob.OnSuccess
			node.OnFailure = bareJob.OnFailure
		}

		wo.dependencies[job.GetName()] = node
	}

	// Second pass: build dependent relationships
	for jobName, node := range wo.dependencies {
		for _, dep := range node.Dependencies {
			depNode, exists := wo.dependencies[dep]
			if !exists {
				return fmt.Errorf("%w: job %s depends on %s", ErrJobNotFound, jobName, dep)
			}
			depNode.Dependents = append(depNode.Dependents, jobName)
		}
	}

	// Validate for circular dependencies
	if err := wo.validateDAG(); err != nil {
		return fmt.Errorf("%w: %w", ErrWorkflowInvalid, err)
	}

	return nil
}

// validateDAG validates that the dependency graph is a Directed Acyclic Graph
func (wo *WorkflowOrchestrator) validateDAG() error {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for jobName := range wo.dependencies {
		if !visited[jobName] {
			if wo.hasCycle(jobName, visited, recStack) {
				return fmt.Errorf("%w: involving job %s", ErrCircularDependency, jobName)
			}
		}
	}

	return nil
}

// hasCycle checks for cycles using DFS
func (wo *WorkflowOrchestrator) hasCycle(jobName string, visited, recStack map[string]bool) bool {
	visited[jobName] = true
	recStack[jobName] = true

	node := wo.dependencies[jobName]
	for _, dep := range node.Dependencies {
		if !visited[dep] {
			if wo.hasCycle(dep, visited, recStack) {
				return true
			}
		} else if recStack[dep] {
			return true
		}
	}

	recStack[jobName] = false
	return false
}

// CanExecute checks if a job can execute based on its dependencies
func (wo *WorkflowOrchestrator) CanExecute(jobName string, executionID string) bool {
	wo.mu.RLock()
	node, exists := wo.dependencies[jobName]
	wo.mu.RUnlock()

	if !exists {
		return true // No dependencies defined
	}

	// Check if all dependencies are satisfied
	execution := wo.getOrCreateExecution(executionID)
	execution.mu.RLock()
	defer execution.mu.RUnlock()

	for _, dep := range node.Dependencies {
		if !execution.CompletedJobs[dep] {
			// Dependency not yet completed
			if execution.FailedJobs[dep] {
				wo.logger.Warningf("Job %s cannot run: dependency %s failed", jobName, dep)
				return false
			}
			wo.logger.Debugf("Job %s waiting for dependency %s", jobName, dep)
			return false
		}
	}

	// Check if job allows parallel execution
	if execution.RunningJobs[jobName] {
		// Check if this job allows parallel execution
		if bareJob, ok := node.Job.(*BareJob); ok && !bareJob.AllowParallel {
			wo.logger.Debugf("Job %s already running, parallel execution not allowed", jobName)
			return false
		}
	}

	return true
}

// JobStarted marks a job as started in the workflow
func (wo *WorkflowOrchestrator) JobStarted(jobName string, executionID string) {
	execution := wo.getOrCreateExecution(executionID)
	execution.mu.Lock()
	defer execution.mu.Unlock()

	execution.RunningJobs[jobName] = true
	wo.logger.Debugf("Workflow %s: Job %s started", executionID, jobName)
}

// JobCompleted marks a job as completed and triggers dependent jobs
func (wo *WorkflowOrchestrator) JobCompleted(ctx context.Context, jobName string, executionID string, success bool) {
	execution := wo.getOrCreateExecution(executionID)
	execution.mu.Lock()
	delete(execution.RunningJobs, jobName)

	if success {
		execution.CompletedJobs[jobName] = true
		wo.logger.Noticef("Workflow %s: Job %s completed successfully", executionID, jobName)
	} else {
		execution.FailedJobs[jobName] = true
		wo.logger.Warningf("Workflow %s: Job %s failed", executionID, jobName)
	}
	execution.mu.Unlock()

	// Trigger dependent jobs
	wo.triggerDependentJobs(ctx, jobName, executionID, success)
}

// triggerDependentJobs triggers jobs that depend on the completed job
func (wo *WorkflowOrchestrator) triggerDependentJobs(ctx context.Context, jobName string, executionID string, success bool) {
	wo.mu.RLock()
	node, exists := wo.dependencies[jobName]
	wo.mu.RUnlock()

	if !exists {
		return
	}

	// Trigger OnSuccess or OnFailure jobs
	var jobsToTrigger []string
	if success {
		jobsToTrigger = node.OnSuccess
	} else {
		jobsToTrigger = node.OnFailure
	}

	for _, triggerJob := range jobsToTrigger {
		if wo.CanExecute(triggerJob, executionID) {
			wo.logger.Noticef("Triggering job %s from workflow", triggerJob)
			_ = wo.scheduler.RunJob(ctx, triggerJob)
		}
	}

	// Check if any dependent jobs can now run
	if success {
		for _, dependent := range node.Dependents {
			if wo.CanExecute(dependent, executionID) {
				wo.logger.Noticef("Dependency satisfied, triggering job %s", dependent)
				_ = wo.scheduler.RunJob(ctx, dependent)
			}
		}
	}
}

// getOrCreateExecution gets or creates a workflow execution
func (wo *WorkflowOrchestrator) getOrCreateExecution(executionID string) *WorkflowExecution {
	wo.mu.Lock()
	defer wo.mu.Unlock()

	if execution, exists := wo.executions[executionID]; exists {
		return execution
	}

	execution := &WorkflowExecution{
		ID:            executionID,
		StartTime:     time.Now(),
		CompletedJobs: make(map[string]bool),
		FailedJobs:    make(map[string]bool),
		RunningJobs:   make(map[string]bool),
	}
	wo.executions[executionID] = execution
	return execution
}

// CleanupOldExecutions removes old workflow executions
func (wo *WorkflowOrchestrator) CleanupOldExecutions(maxAge time.Duration) {
	wo.mu.Lock()
	defer wo.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, execution := range wo.executions {
		if execution.StartTime.Before(cutoff) {
			delete(wo.executions, id)
			wo.logger.Debugf("Cleaned up old workflow execution %s", id)
		}
	}
}

// GetWorkflowStatus returns the status of a workflow execution
func (wo *WorkflowOrchestrator) GetWorkflowStatus(executionID string) map[string]any {
	wo.mu.RLock()
	execution, exists := wo.executions[executionID]
	wo.mu.RUnlock()

	if !exists {
		return nil
	}

	execution.mu.RLock()
	defer execution.mu.RUnlock()

	return map[string]any{
		"id":            execution.ID,
		"startTime":     execution.StartTime,
		"duration":      time.Since(execution.StartTime),
		"completedJobs": len(execution.CompletedJobs),
		"failedJobs":    len(execution.FailedJobs),
		"runningJobs":   len(execution.RunningJobs),
		"completed":     execution.CompletedJobs,
		"failed":        execution.FailedJobs,
		"running":       execution.RunningJobs,
	}
}
