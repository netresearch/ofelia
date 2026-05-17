// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

// Defense-in-depth regression tests for #623. Every public method on every
// *ServiceAdapter in this package must return ErrNilDockerClient — never
// panic — when the embedded SDK client is nil. The exported constructors
// always wire a non-nil client, so this is only reachable via hand-rolled
// adapter values (test fixtures or wiring bugs); the guards convert what
// would otherwise be a `nil pointer dereference` in a hot goroutine into a
// branchable, actionable error.

// assertErrNilDockerClient is a tiny helper that fails the test if err is
// nil or doesn't wrap ErrNilDockerClient. Keeps the table rows compact.
func assertErrNilDockerClient(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: expected ErrNilDockerClient, got nil", name)
		return
	}
	if !errors.Is(err, ErrNilDockerClient) {
		t.Errorf("%s: expected errors.Is(err, ErrNilDockerClient), got: %v", name, err)
	}
}

// TestContainerServiceAdapter_NilClient_NoPanic iterates every public
// method on a zero-valued ContainerServiceAdapter and asserts each returns
// ErrNilDockerClient (rather than panicking on a nil-pointer dereference).
func TestContainerServiceAdapter_NilClient_NoPanic(t *testing.T) {
	t.Parallel()
	defer failOnPanic(t, "ContainerServiceAdapter nil-client invocation")()

	a := &ContainerServiceAdapter{}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"Create", func() error {
			_, err := a.Create(ctx, &domain.ContainerConfig{})
			return err
		}},
		{"Start", func() error { return a.Start(ctx, "id") }},
		{"Stop", func() error { return a.Stop(ctx, "id", domain.StopOptions{}) }},
		{"Remove", func() error { return a.Remove(ctx, "id", domain.RemoveOptions{}) }},
		{"Inspect", func() error {
			_, err := a.Inspect(ctx, "id")
			return err
		}},
		{"List", func() error {
			_, err := a.List(ctx, domain.ListOptions{})
			return err
		}},
		{"Logs", func() error {
			_, err := a.Logs(ctx, "id", domain.LogOptions{})
			return err
		}},
		{"CopyLogs", func() error { return a.CopyLogs(ctx, "id", nil, nil, domain.LogOptions{}) }},
		{"Kill", func() error { return a.Kill(ctx, "id", "SIGTERM") }},
		{"Pause", func() error { return a.Pause(ctx, "id") }},
		{"Unpause", func() error { return a.Unpause(ctx, "id") }},
		{"Rename", func() error { return a.Rename(ctx, "id", "new") }},
		{"Attach", func() error {
			_, err := a.Attach(ctx, "id", ports.AttachOptions{})
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertErrNilDockerClient(t, tc.name, tc.call())
		})
	}

	// Wait returns channels — drain errCh, assert it carried the sentinel,
	// and assert both channels are closed afterward so callers' select loops
	// cannot leak (regression guard for #623 contract).
	t.Run("Wait", func(t *testing.T) {
		respCh, errCh := a.Wait(ctx, "id")
		select {
		case err := <-errCh:
			assertErrNilDockerClient(t, "Wait", err)
		case <-time.After(time.Second):
			t.Fatal("Wait: errCh did not deliver ErrNilDockerClient within 1s")
		}
		select {
		case _, ok := <-errCh:
			if ok {
				t.Fatal("Wait: errCh delivered a second value; expected close after the sentinel")
			}
		case <-time.After(time.Second):
			t.Fatal("Wait: errCh was not closed after delivering ErrNilDockerClient")
		}
		select {
		case _, ok := <-respCh:
			if ok {
				t.Fatal("Wait: respCh delivered a value; expected close on nil-client path")
			}
		case <-time.After(time.Second):
			t.Fatal("Wait: respCh was not closed on nil-client path")
		}
	})
}

// TestExecServiceAdapter_NilClient_NoPanic asserts every method on
// ExecServiceAdapter returns ErrNilDockerClient when the SDK client is nil.
//
// Note: the existing TestExecServiceAdapter_Create_NilConfig and
// TestExecServiceAdapter_Run_NilWritersNonTTY tests now use a loopback
// SDK client to keep their original input-validation contracts alive after
// the nil-client guard was added at the top of Create/Run.
func TestExecServiceAdapter_NilClient_NoPanic(t *testing.T) {
	t.Parallel()
	defer failOnPanic(t, "ExecServiceAdapter nil-client invocation")()

	a := &ExecServiceAdapter{}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"Create", func() error {
			_, err := a.Create(ctx, "cid", &domain.ExecConfig{})
			return err
		}},
		{"Start", func() error {
			_, err := a.Start(ctx, "eid", domain.ExecStartOptions{})
			return err
		}},
		{"Inspect", func() error {
			_, err := a.Inspect(ctx, "eid")
			return err
		}},
		{"Run", func() error {
			_, err := a.Run(ctx, "cid", &domain.ExecConfig{}, nil, nil)
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertErrNilDockerClient(t, tc.name, tc.call())
		})
	}
}

// TestImageServiceAdapter_NilClient_NoPanic asserts every method on
// ImageServiceAdapter returns ErrNilDockerClient when the SDK client is nil.
func TestImageServiceAdapter_NilClient_NoPanic(t *testing.T) {
	t.Parallel()
	defer failOnPanic(t, "ImageServiceAdapter nil-client invocation")()

	a := &ImageServiceAdapter{}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"Pull", func() error {
			_, err := a.Pull(ctx, domain.PullOptions{Repository: "r"})
			return err
		}},
		{"PullAndWait", func() error { return a.PullAndWait(ctx, domain.PullOptions{Repository: "r"}) }},
		{"List", func() error {
			_, err := a.List(ctx, domain.ImageListOptions{})
			return err
		}},
		{"Inspect", func() error {
			_, err := a.Inspect(ctx, "img")
			return err
		}},
		{"Remove", func() error { return a.Remove(ctx, "img", false, false) }},
		{"Tag", func() error { return a.Tag(ctx, "src", "dst") }},
		{"Exists", func() error {
			_, err := a.Exists(ctx, "img")
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertErrNilDockerClient(t, tc.name, tc.call())
		})
	}
}

// TestEventServiceAdapter_NilClient_NoPanic covers Subscribe (channel-
// returning, no error return — sentinel must arrive on errCh and both
// channels must be closed) and SubscribeWithCallback.
func TestEventServiceAdapter_NilClient_NoPanic(t *testing.T) {
	t.Parallel()
	defer failOnPanic(t, "EventServiceAdapter nil-client invocation")()

	a := &EventServiceAdapter{}
	ctx := context.Background()

	t.Run("Subscribe", func(t *testing.T) {
		eventCh, errCh := a.Subscribe(ctx, domain.EventFilter{})
		select {
		case err := <-errCh:
			assertErrNilDockerClient(t, "Subscribe", err)
		case <-time.After(time.Second):
			t.Fatal("Subscribe: errCh did not deliver ErrNilDockerClient within 1s")
		}
		// Both channels must be closed so the caller's loop can exit
		// cleanly without leaking a goroutine.
		if _, ok := <-eventCh; ok {
			t.Error("Subscribe: eventCh should be closed when client is nil")
		}
		if _, ok := <-errCh; ok {
			t.Error("Subscribe: errCh should be closed after error delivery")
		}
	})

	t.Run("SubscribeWithCallback", func(t *testing.T) {
		err := a.SubscribeWithCallback(ctx, domain.EventFilter{}, func(domain.Event) error { return nil })
		assertErrNilDockerClient(t, "SubscribeWithCallback", err)
	})
}

// TestNetworkServiceAdapter_NilClient_NoPanic asserts every method on
// NetworkServiceAdapter returns ErrNilDockerClient when the SDK client is nil.
func TestNetworkServiceAdapter_NilClient_NoPanic(t *testing.T) {
	t.Parallel()
	defer failOnPanic(t, "NetworkServiceAdapter nil-client invocation")()

	a := &NetworkServiceAdapter{}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"Connect", func() error { return a.Connect(ctx, "nid", "cid", nil) }},
		{"Disconnect", func() error { return a.Disconnect(ctx, "nid", "cid", false) }},
		{"List", func() error {
			_, err := a.List(ctx, domain.NetworkListOptions{})
			return err
		}},
		{"Inspect", func() error {
			_, err := a.Inspect(ctx, "nid")
			return err
		}},
		{"Create", func() error {
			_, err := a.Create(ctx, "name", ports.NetworkCreateOptions{})
			return err
		}},
		{"Remove", func() error { return a.Remove(ctx, "nid") }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertErrNilDockerClient(t, tc.name, tc.call())
		})
	}
}

// TestSwarmServiceAdapter_NilClient_NoPanic asserts every method on
// SwarmServiceAdapter returns ErrNilDockerClient when the SDK client is nil.
func TestSwarmServiceAdapter_NilClient_NoPanic(t *testing.T) {
	t.Parallel()
	defer failOnPanic(t, "SwarmServiceAdapter nil-client invocation")()

	a := &SwarmServiceAdapter{}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"Create", func() error {
			_, err := a.Create(ctx, domain.ServiceSpec{}, domain.ServiceCreateOptions{})
			return err
		}},
		{"Inspect", func() error {
			_, err := a.Inspect(ctx, "sid")
			return err
		}},
		{"List", func() error {
			_, err := a.List(ctx, domain.ServiceListOptions{})
			return err
		}},
		{"Remove", func() error { return a.Remove(ctx, "sid") }},
		{"ListTasks", func() error {
			_, err := a.ListTasks(ctx, domain.TaskListOptions{})
			return err
		}},
		{"WaitForTask", func() error {
			_, err := a.WaitForTask(ctx, "tid", time.Millisecond)
			return err
		}},
		{"WaitForServiceTasks", func() error {
			_, err := a.WaitForServiceTasks(ctx, "sid", time.Millisecond)
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertErrNilDockerClient(t, tc.name, tc.call())
		})
	}
}

// TestSystemServiceAdapter_NilClient_NoPanic asserts every method on
// SystemServiceAdapter returns ErrNilDockerClient when the SDK client is nil.
func TestSystemServiceAdapter_NilClient_NoPanic(t *testing.T) {
	t.Parallel()
	defer failOnPanic(t, "SystemServiceAdapter nil-client invocation")()

	a := &SystemServiceAdapter{}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"Info", func() error {
			_, err := a.Info(ctx)
			return err
		}},
		{"Ping", func() error {
			_, err := a.Ping(ctx)
			return err
		}},
		{"Version", func() error {
			_, err := a.Version(ctx)
			return err
		}},
		{"DiskUsage", func() error {
			_, err := a.DiskUsage(ctx)
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertErrNilDockerClient(t, tc.name, tc.call())
		})
	}
}
