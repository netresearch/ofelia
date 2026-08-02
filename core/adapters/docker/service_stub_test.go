// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/domain"
)

// The swarm adapter's polling loops (WaitForTask, WaitForServiceTasks) were
// only reachable with a swarm-enabled daemon, so their termination conditions
// — task reached a terminal state, timeout expired — went unexercised. A stub
// daemon lets both be driven deterministically.

// swarmTask builds one entry of a /tasks response in the shape the SDK decodes.
func swarmTask(id, state string) map[string]any {
	return map[string]any{
		"ID":     id,
		"Status": map[string]any{"State": state},
	}
}

func TestSwarmServiceAdapter_Create_ReturnsID(t *testing.T) {
	t.Parallel()

	adapter := &SwarmServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/services/create": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{"ID": "svc-123", "Warnings": []string{}})
		},
	})}

	id, err := adapter.Create(context.Background(), domain.ServiceSpec{Name: "ofelia-job"},
		domain.ServiceCreateOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id != "svc-123" {
		t.Errorf("Create returned %q, want svc-123", id)
	}
}

func TestSwarmServiceAdapter_List_ConvertsServices(t *testing.T) {
	t.Parallel()

	adapter := &SwarmServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/services": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, []map[string]any{
				{"ID": "svc-1", "Spec": map[string]any{"Name": "first"}},
				{"ID": "svc-2", "Spec": map[string]any{"Name": "second"}},
			})
		},
	})}

	got, err := adapter.List(context.Background(), domain.ServiceListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d services, want 2", len(got))
	}
	if got[0].ID != "svc-1" || got[1].ID != "svc-2" {
		t.Errorf("List returned %q, %q; want svc-1, svc-2", got[0].ID, got[1].ID)
	}
}

func TestSwarmServiceAdapter_ListTasks_ConvertsTasks(t *testing.T) {
	t.Parallel()

	adapter := &SwarmServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/tasks": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, []map[string]any{
				swarmTask("task-1", string(domain.TaskStateRunning)),
				swarmTask("task-2", string(domain.TaskStateComplete)),
			})
		},
	})}

	got, err := adapter.ListTasks(context.Background(), domain.TaskListOptions{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListTasks returned %d tasks, want 2", len(got))
	}
	if got[0].ID != "task-1" || got[1].ID != "task-2" {
		t.Errorf("ListTasks returned %q, %q; want task-1, task-2", got[0].ID, got[1].ID)
	}
}

func TestSwarmServiceAdapter_Inspect_ReturnsService(t *testing.T) {
	t.Parallel()

	adapter := &SwarmServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/services/": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{
				"ID":   "svc-123",
				"Spec": map[string]any{"Name": "inspected"},
			})
		},
	})}

	got, err := adapter.Inspect(context.Background(), "svc-123")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if got.ID != "svc-123" {
		t.Errorf("Inspect returned id %q, want svc-123", got.ID)
	}
}

func TestSwarmServiceAdapter_Remove_Succeeds(t *testing.T) {
	t.Parallel()

	adapter := &SwarmServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/services/": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})}

	if err := adapter.Remove(context.Background(), "svc-123"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

// TestSwarmServiceAdapter_WaitForTask_ReturnsOnTerminalState drives the polling
// loop to its success exit: the first poll reports a running task, the second a
// completed one, and only then does WaitForTask return.
func TestSwarmServiceAdapter_WaitForTask_ReturnsOnTerminalState(t *testing.T) {
	t.Parallel()

	poll := 0
	adapter := &SwarmServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/tasks": func(w http.ResponseWriter, _ *http.Request) {
			poll++
			state := string(domain.TaskStateRunning)
			if poll > 1 {
				state = string(domain.TaskStateComplete)
			}
			writeJSON(t, w, []map[string]any{swarmTask("task-1", state)})
		},
	})}

	got, err := adapter.WaitForTask(context.Background(), "task-1", 10*time.Second)
	if err != nil {
		t.Fatalf("WaitForTask: %v", err)
	}
	if got.ID != "task-1" {
		t.Errorf("WaitForTask returned task %q, want task-1", got.ID)
	}
	if poll < 2 {
		t.Errorf("WaitForTask returned after %d polls; it should keep polling while the task runs", poll)
	}
}

// TestSwarmServiceAdapter_WaitForTask_TimesOut pins the other exit: a task that
// never terminates must surface domain.ErrTimeout rather than blocking, which
// is what stops a wedged swarm job from hanging the scheduler.
func TestSwarmServiceAdapter_WaitForTask_TimesOut(t *testing.T) {
	t.Parallel()

	adapter := &SwarmServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/tasks": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, []map[string]any{swarmTask("task-1", string(domain.TaskStateRunning))})
		},
	})}

	_, err := adapter.WaitForTask(context.Background(), "task-1", 1200*time.Millisecond)
	if !errors.Is(err, domain.ErrTimeout) {
		t.Fatalf("WaitForTask error = %v, want domain.ErrTimeout", err)
	}
}

// TestSwarmServiceAdapter_WaitForServiceTasks_WaitsForAll pins that the wait
// ends only once EVERY task is terminal — a service whose second replica is
// still running must not be reported as finished.
func TestSwarmServiceAdapter_WaitForServiceTasks_WaitsForAll(t *testing.T) {
	t.Parallel()

	poll := 0
	adapter := &SwarmServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/tasks": func(w http.ResponseWriter, _ *http.Request) {
			poll++
			second := string(domain.TaskStateRunning)
			if poll > 1 {
				second = string(domain.TaskStateFailed)
			}
			writeJSON(t, w, []map[string]any{
				swarmTask("task-1", string(domain.TaskStateComplete)),
				swarmTask("task-2", second),
			})
		},
	})}

	got, err := adapter.WaitForServiceTasks(context.Background(), "svc-1", 10*time.Second)
	if err != nil {
		t.Fatalf("WaitForServiceTasks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("WaitForServiceTasks returned %d tasks, want 2", len(got))
	}
	if poll < 2 {
		t.Error("WaitForServiceTasks returned while one task was still running")
	}
}

// TestAllTasksTerminal covers the helper directly, including the empty slice,
// which the polling loops never reach because they skip empty results.
func TestAllTasksTerminal(t *testing.T) {
	t.Parallel()

	terminal := domain.Task{Status: domain.TaskStatus{State: domain.TaskStateComplete}}
	running := domain.Task{Status: domain.TaskStatus{State: domain.TaskStateRunning}}

	cases := []struct {
		name  string
		tasks []domain.Task
		want  bool
	}{
		{name: "empty is vacuously true", tasks: nil, want: true},
		{name: "all terminal", tasks: []domain.Task{terminal, terminal}, want: true},
		{name: "one still running", tasks: []domain.Task{terminal, running}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := allTasksTerminal(tc.tasks); got != tc.want {
				t.Errorf("allTasksTerminal = %v, want %v", got, tc.want)
			}
		})
	}
}
