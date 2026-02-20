package core

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/netresearch/go-cron"
)

// dependencyEdge represents a single dependency edge to be wired via go-cron.
type dependencyEdge struct {
	child     string // child job name
	parent    string // parent job name
	condition cron.TriggerCondition
}

// BuildWorkflowDependencies analyzes job dependency configuration and wires
// dependency edges into the go-cron scheduler using AddDependencyByName.
//
// Mapping from ofelia config to go-cron trigger conditions:
//   - depends-on: child.After(parent, OnSuccess) -- wait for parent to succeed
//   - on-success: target.After(source, OnSuccess) -- trigger target when source succeeds
//   - on-failure: target.After(source, OnFailure) -- trigger target when source fails
//
// go-cron handles cycle detection, execution ordering, and status tracking natively.
func BuildWorkflowDependencies(cronInstance *cron.Cron, jobs []Job, logger *slog.Logger) error {
	edges := collectDependencyEdges(jobs, logger)
	if len(edges) == 0 {
		return nil
	}

	// Validate that all referenced job names exist as cron entries
	if err := validateEdgeTargets(cronInstance, edges); err != nil {
		return err
	}

	// Wire edges into go-cron
	return wireEdges(cronInstance, edges, logger)
}

// collectDependencyEdges extracts dependency edges from BareJob configuration fields.
func collectDependencyEdges(jobs []Job, logger *slog.Logger) []dependencyEdge {
	var edges []dependencyEdge

	for _, job := range jobs {
		bareJob, ok := job.(*BareJob)
		if !ok {
			continue
		}

		jobName := bareJob.GetName()

		// depends-on: this job depends on the listed parents succeeding
		for _, parent := range bareJob.Dependencies {
			edges = append(edges, dependencyEdge{
				child:     jobName,
				parent:    parent,
				condition: cron.OnSuccess,
			})
			logger.Debug(fmt.Sprintf("Workflow edge: %q depends on %q (OnSuccess)", jobName, parent))
		}

		// on-success: listed jobs should run when this job succeeds
		for _, target := range bareJob.OnSuccess {
			edges = append(edges, dependencyEdge{
				child:     target,
				parent:    jobName,
				condition: cron.OnSuccess,
			})
			logger.Debug(fmt.Sprintf("Workflow edge: %q triggered by %q (OnSuccess)", target, jobName))
		}

		// on-failure: listed jobs should run when this job fails
		for _, target := range bareJob.OnFailure {
			edges = append(edges, dependencyEdge{
				child:     target,
				parent:    jobName,
				condition: cron.OnFailure,
			})
			logger.Debug(fmt.Sprintf("Workflow edge: %q triggered by %q (OnFailure)", target, jobName))
		}
	}

	return edges
}

// validateEdgeTargets checks that all job names referenced in edges exist as cron entries.
func validateEdgeTargets(cronInstance *cron.Cron, edges []dependencyEdge) error {
	for _, edge := range edges {
		if entry := cronInstance.EntryByName(edge.parent); !entry.Valid() {
			return fmt.Errorf("%w: job %q references unknown parent %q", ErrJobNotFound, edge.child, edge.parent)
		}
		if entry := cronInstance.EntryByName(edge.child); !entry.Valid() {
			return fmt.Errorf("%w: job %q references unknown child %q", ErrJobNotFound, edge.parent, edge.child)
		}
	}
	return nil
}

// wireEdges registers all dependency edges with go-cron via AddDependencyByName.
// go-cron performs cycle detection and returns ErrCycleDetected if a cycle is found.
func wireEdges(cronInstance *cron.Cron, edges []dependencyEdge, logger *slog.Logger) error {
	for _, edge := range edges {
		err := cronInstance.AddDependencyByName(edge.child, edge.parent, edge.condition)
		if err != nil {
			if errors.Is(err, cron.ErrCycleDetected) {
				return fmt.Errorf("%w: %w (edge: %s -> %s)", ErrCircularDependency, err, edge.parent, edge.child)
			}
			return fmt.Errorf("%w: failed to add dependency %s -> %s: %w",
				ErrWorkflowInvalid, edge.parent, edge.child, err)
		}
		logger.Info(fmt.Sprintf("Wired dependency: %s -> %s (%s)", edge.parent, edge.child, edge.condition))
	}
	return nil
}
