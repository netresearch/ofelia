package cli

import (
	"context"
	"net"
	"testing"
	"time"
)

func getUnusedAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get unused port: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()
	return addr
}

func TestWaitForServerWithErrChan_Success(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create test listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := waitForServerWithErrChan(ctx, addr, nil); err != nil {
		t.Errorf("waitForServerWithErrChan failed: %v", err)
	}
}

func TestWaitForServerWithErrChan_Timeout(t *testing.T) {
	addr := getUnusedAddr(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := waitForServerWithErrChan(ctx, addr, nil)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	if ctx.Err() != context.DeadlineExceeded {
		t.Errorf("Expected context deadline exceeded, got: %v", ctx.Err())
	}
}

func TestWaitForServerWithErrChan_DelayedStart(t *testing.T) {
	addr := "127.0.0.1:0"
	tempListener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to reserve port: %v", err)
	}
	actualAddr := tempListener.Addr().String()
	tempListener.Close()

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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := waitForServerWithErrChan(ctx, actualAddr, nil); err != nil {
		t.Errorf("waitForServerWithErrChan failed for delayed server: %v", err)
	}
}

func TestWaitForServerWithErrChan_CancelContext(t *testing.T) {
	addr := getUnusedAddr(t)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := waitForServerWithErrChan(ctx, addr, nil)
	if err == nil {
		t.Error("Expected cancellation error, got nil")
	}

	if ctx.Err() != context.Canceled {
		t.Errorf("Expected context canceled, got: %v", ctx.Err())
	}
}
