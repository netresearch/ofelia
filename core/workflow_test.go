package core

import (
	"errors"
	"testing"

	"github.com/netresearch/go-cron"
)

// TestWorkflowDependencies tests that depends-on edges are wired correctly
func TestWorkflowDependencies(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	jobA := &BareJob{Name: "job-a", Schedule: "@daily", Command: "echo A"}
	jobB := &BareJob{Name: "job-b", Schedule: "@daily", Command: "echo B", Dependencies: []string{"job-a"}}
	jobC := &BareJob{Name: "job-c", Schedule: "@daily", Command: "echo C", Dependencies: []string{"job-a", "job-b"}}

	_ = sc.AddJob(jobA)
	_ = sc.AddJob(jobB)
	_ = sc.AddJob(jobC)

	err := BuildWorkflowDependencies(sc.cron, sc.Jobs, sc.Logger)
	if err != nil {
		t.Fatalf("BuildWorkflowDependencies failed: %v", err)
	}

	// Verify job-b depends on job-a
	entryB := sc.cron.EntryByName("job-b")
	if !entryB.Valid() {
		t.Fatal("job-b entry should exist")
	}
	depsB := sc.cron.Dependencies(entryB.ID)
	if len(depsB) != 1 {
		t.Fatalf("job-b should have 1 dependency, got %d", len(depsB))
	}

	entryA := sc.cron.EntryByName("job-a")
	if depsB[0].ParentID != entryA.ID {
		t.Error("job-b's dependency should be job-a")
	}
	if depsB[0].Condition != cron.OnSuccess {
		t.Errorf("job-b's dependency condition should be OnSuccess, got %v", depsB[0].Condition)
	}

	// Verify job-c depends on both job-a and job-b
	entryC := sc.cron.EntryByName("job-c")
	if !entryC.Valid() {
		t.Fatal("job-c entry should exist")
	}
	depsC := sc.cron.Dependencies(entryC.ID)
	if len(depsC) != 2 {
		t.Fatalf("job-c should have 2 dependencies, got %d", len(depsC))
	}

	parentIDs := map[cron.EntryID]bool{}
	for _, dep := range depsC {
		parentIDs[dep.ParentID] = true
	}
	if !parentIDs[entryA.ID] {
		t.Error("job-c should depend on job-a")
	}
	entryBID := sc.cron.EntryByName("job-b").ID
	if !parentIDs[entryBID] {
		t.Error("job-c should depend on job-b")
	}
}

// TestCircularDependencyDetection tests detection of circular dependencies via go-cron
func TestCircularDependencyDetection(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	jobA := &BareJob{Name: "job-a", Schedule: "@daily", Command: "echo A", Dependencies: []string{"job-c"}}
	jobB := &BareJob{Name: "job-b", Schedule: "@daily", Command: "echo B", Dependencies: []string{"job-a"}}
	jobC := &BareJob{Name: "job-c", Schedule: "@daily", Command: "echo C", Dependencies: []string{"job-b"}}

	_ = sc.AddJob(jobA)
	_ = sc.AddJob(jobB)
	_ = sc.AddJob(jobC)

	err := BuildWorkflowDependencies(sc.cron, sc.Jobs, sc.Logger)
	if err == nil {
		t.Fatal("Should have detected circular dependency")
	}

	if !errors.Is(err, ErrCircularDependency) {
		t.Errorf("Error should be ErrCircularDependency, got: %v", err)
	}
}

// TestOnSuccessOnFailureTriggers tests that on-success/on-failure edges are wired correctly
func TestOnSuccessOnFailureTriggers(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	jobMain := &BareJob{
		Name:      "job-main",
		Schedule:  "@daily",
		Command:   "echo main",
		OnSuccess: []string{"job-success"},
		OnFailure: []string{"job-failure"},
	}
	jobSuccess := &BareJob{Name: "job-success", Schedule: "@daily", Command: "echo success"}
	jobFailure := &BareJob{Name: "job-failure", Schedule: "@daily", Command: "echo failure"}

	_ = sc.AddJob(jobMain)
	_ = sc.AddJob(jobSuccess)
	_ = sc.AddJob(jobFailure)

	err := BuildWorkflowDependencies(sc.cron, sc.Jobs, sc.Logger)
	if err != nil {
		t.Fatalf("BuildWorkflowDependencies failed: %v", err)
	}

	// Verify job-success has OnSuccess dependency on job-main
	successEntry := sc.cron.EntryByName("job-success")
	if !successEntry.Valid() {
		t.Fatal("job-success entry should exist")
	}
	successDeps := sc.cron.Dependencies(successEntry.ID)
	if len(successDeps) != 1 {
		t.Fatalf("job-success should have 1 dependency, got %d", len(successDeps))
	}
	mainEntry := sc.cron.EntryByName("job-main")
	if successDeps[0].ParentID != mainEntry.ID {
		t.Error("job-success should depend on job-main")
	}
	if successDeps[0].Condition != cron.OnSuccess {
		t.Errorf("Expected OnSuccess condition, got %v", successDeps[0].Condition)
	}

	// Verify job-failure has OnFailure dependency on job-main
	failureEntry := sc.cron.EntryByName("job-failure")
	if !failureEntry.Valid() {
		t.Fatal("job-failure entry should exist")
	}
	failureDeps := sc.cron.Dependencies(failureEntry.ID)
	if len(failureDeps) != 1 {
		t.Fatalf("job-failure should have 1 dependency, got %d", len(failureDeps))
	}
	if failureDeps[0].ParentID != mainEntry.ID {
		t.Error("job-failure should depend on job-main")
	}
	if failureDeps[0].Condition != cron.OnFailure {
		t.Errorf("Expected OnFailure condition, got %v", failureDeps[0].Condition)
	}
}

// TestNoDependencyJobs tests that jobs without dependencies work normally
func TestNoDependencyJobs(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	job := &BareJob{Name: "standalone", Schedule: "@daily", Command: "echo standalone"}
	_ = sc.AddJob(job)

	err := BuildWorkflowDependencies(sc.cron, sc.Jobs, sc.Logger)
	if err != nil {
		t.Fatalf("BuildWorkflowDependencies should succeed for jobs without dependencies: %v", err)
	}

	entry := sc.cron.EntryByName("standalone")
	if !entry.Valid() {
		t.Fatal("standalone entry should exist")
	}
	deps := sc.cron.Dependencies(entry.ID)
	if len(deps) != 0 {
		t.Errorf("standalone job should have no dependencies, got %d", len(deps))
	}
}

// TestMissingDependencyTarget tests that referencing a non-existent job fails
func TestMissingDependencyTarget(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	job := &BareJob{
		Name:         "orphan",
		Schedule:     "@daily",
		Command:      "echo orphan",
		Dependencies: []string{"nonexistent"},
	}
	_ = sc.AddJob(job)

	err := BuildWorkflowDependencies(sc.cron, sc.Jobs, sc.Logger)
	if err == nil {
		t.Fatal("Should fail when referencing nonexistent dependency")
	}
	if !errors.Is(err, ErrJobNotFound) {
		t.Errorf("Error should be ErrJobNotFound, got: %v", err)
	}
}

// TestMixedDependencyTypes tests jobs with both depends-on and on-success/on-failure
func TestMixedDependencyTypes(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	jobA := &BareJob{
		Name:      "job-a",
		Schedule:  "@daily",
		Command:   "echo A",
		OnSuccess: []string{"job-c"},
	}
	jobB := &BareJob{
		Name:     "job-b",
		Schedule: "@daily",
		Command:  "echo B",
	}
	jobC := &BareJob{
		Name:         "job-c",
		Schedule:     "@daily",
		Command:      "echo C",
		Dependencies: []string{"job-b"}, // depends-on job-b AND triggered by job-a on-success
	}

	_ = sc.AddJob(jobA)
	_ = sc.AddJob(jobB)
	_ = sc.AddJob(jobC)

	err := BuildWorkflowDependencies(sc.cron, sc.Jobs, sc.Logger)
	if err != nil {
		t.Fatalf("BuildWorkflowDependencies failed: %v", err)
	}

	// job-c should have 2 dependencies: job-b (from depends-on) and job-a (from on-success)
	entryC := sc.cron.EntryByName("job-c")
	depsC := sc.cron.Dependencies(entryC.ID)
	if len(depsC) != 2 {
		t.Fatalf("job-c should have 2 dependencies, got %d", len(depsC))
	}
}

// TestCollectDependencyEdges tests the internal edge collection logic
func TestCollectDependencyEdges(t *testing.T) {
	t.Parallel()

	jobs := []Job{
		&BareJob{
			Name:         "child",
			Schedule:     "@daily",
			Command:      "echo child",
			Dependencies: []string{"parent"},
			OnSuccess:    []string{"success-handler"},
			OnFailure:    []string{"failure-handler"},
		},
		&BareJob{Name: "parent", Schedule: "@daily", Command: "echo parent"},
		&BareJob{Name: "success-handler", Schedule: "@daily", Command: "echo success"},
		&BareJob{Name: "failure-handler", Schedule: "@daily", Command: "echo failure"},
	}

	edges := collectDependencyEdges(jobs, newDiscardLogger())

	// Should have 3 edges: 1 depends-on, 1 on-success, 1 on-failure
	if len(edges) != 3 {
		t.Fatalf("Expected 3 edges, got %d", len(edges))
	}

	// Verify edge types
	edgeMap := map[string]cron.TriggerCondition{}
	for _, e := range edges {
		key := e.parent + "->" + e.child
		edgeMap[key] = e.condition
	}

	if cond, ok := edgeMap["parent->child"]; !ok || cond != cron.OnSuccess {
		t.Error("Expected depends-on edge: parent->child (OnSuccess)")
	}
	if cond, ok := edgeMap["child->success-handler"]; !ok || cond != cron.OnSuccess {
		t.Error("Expected on-success edge: child->success-handler (OnSuccess)")
	}
	if cond, ok := edgeMap["child->failure-handler"]; !ok || cond != cron.OnFailure {
		t.Error("Expected on-failure edge: child->failure-handler (OnFailure)")
	}
}

// TestEmptyJobList tests BuildWorkflowDependencies with no jobs
func TestEmptyJobList(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	err := BuildWorkflowDependencies(sc.cron, nil, sc.Logger)
	if err != nil {
		t.Fatalf("Should succeed with nil job list: %v", err)
	}

	err = BuildWorkflowDependencies(sc.cron, []Job{}, sc.Logger)
	if err != nil {
		t.Fatalf("Should succeed with empty job list: %v", err)
	}
}
