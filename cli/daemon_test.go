package cli

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestWaitForServer_Success tests successful server connection
func TestWaitForServer_Success(t *testing.T) {
	// Start a test TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create test listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Wait for server with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := waitForServer(ctx, addr); err != nil {
		t.Errorf("waitForServer failed: %v", err)
	}
}

// TestWaitForServer_Timeout tests timeout when server doesn't start
func TestWaitForServer_Timeout(t *testing.T) {
	// Use an address that will never have a server
	addr := "127.0.0.1:65535"

	// Use a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := waitForServer(ctx, addr)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	if ctx.Err() != context.DeadlineExceeded {
		t.Errorf("Expected context deadline exceeded, got: %v", ctx.Err())
	}
}

// TestWaitForServer_DelayedStart tests server that starts after some delay
func TestWaitForServer_DelayedStart(t *testing.T) {
	addr := "127.0.0.1:0"

	// Reserve port by creating and closing listener
	tempListener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to reserve port: %v", err)
	}
	actualAddr := tempListener.Addr().String()
	tempListener.Close()

	// Start server with delay in background
	go func() {
		time.Sleep(100 * time.Millisecond)
		listener, err := net.Listen("tcp", actualAddr)
		if err != nil {
			t.Logf("Failed to start delayed server: %v", err)
			return
		}
		defer listener.Close()
		time.Sleep(500 * time.Millisecond)
	}()

	// Wait for server with sufficient timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := waitForServer(ctx, actualAddr); err != nil {
		t.Errorf("waitForServer failed for delayed server: %v", err)
	}
}

// TestWaitForServer_CancelContext tests cancellation behavior
func TestWaitForServer_CancelContext(t *testing.T) {
	addr := "127.0.0.1:65534"

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := waitForServer(ctx, addr)
	if err == nil {
		t.Error("Expected cancellation error, got nil")
	}

	if ctx.Err() != context.Canceled {
		t.Errorf("Expected context canceled, got: %v", ctx.Err())
	}
}
