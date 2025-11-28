package core

import (
	"testing"

	"github.com/sirupsen/logrus"
)

// TestBareJobRun tests the BareJob.Run() function that currently has 0% coverage
func TestBareJobRun(t *testing.T) {
	t.Parallel()

	// Create a test job
	job := &BareJob{
		Name:    "test-run-job",
		Command: "echo test",
	}

	// Create test scheduler and context
	logger := &LogrusAdapter{Logger: logrus.New()}
	scheduler := NewScheduler(logger)

	exec, err := NewExecution()
	if err != nil {
		t.Fatal(err)
	}

	ctx := NewContext(scheduler, job, exec)

	// Test the Run method - should call ctx.Next()
	err = job.Run(ctx)
	// For BareJob, Run should always return nil since it just calls ctx.Next()
	// which returns nil when there are no middlewares
	if err != nil {
		t.Errorf("BareJob.Run() returned error: %v", err)
	}
}

// TestBufferPoolGetSized tests the BufferPool.GetSized() function that currently has 0% coverage
func TestBufferPoolGetSized(t *testing.T) {
	t.Parallel()

	// Create a buffer pool: min=100, default=500, max=2000
	pool := NewBufferPool(100, 500, 2000)

	// Test 1: Request size within normal range (should use pool)
	buf1, err := pool.GetSized(300)
	if err != nil {
		t.Fatalf("GetSized(300) error: %v", err)
	}
	if buf1 == nil {
		t.Fatal("GetSized(300) returned nil")
	}
	if buf1.Size() != 500 { // Should get default size buffer from pool
		t.Errorf("Expected buffer size 500, got %d", buf1.Size())
	}

	// Test 2: Request size exactly at minSize boundary
	buf2, err := pool.GetSized(100)
	if err != nil {
		t.Fatalf("GetSized(100) error: %v", err)
	}
	if buf2 == nil {
		t.Fatal("GetSized(100) returned nil")
	}
	if buf2.Size() != 500 { // Should get default size buffer from pool
		t.Errorf("Expected buffer size 500, got %d", buf2.Size())
	}

	// Test 3: Request size exactly at default size boundary
	buf3, err := pool.GetSized(500)
	if err != nil {
		t.Fatalf("GetSized(500) error: %v", err)
	}
	if buf3 == nil {
		t.Fatal("GetSized(500) returned nil")
	}
	if buf3.Size() != 500 { // Should get default size buffer from pool
		t.Errorf("Expected buffer size 500, got %d", buf3.Size())
	}

	// Test 4: Request larger than default but under max (should create custom buffer)
	buf4, err := pool.GetSized(1000)
	if err != nil {
		t.Fatalf("GetSized(1000) error: %v", err)
	}
	if buf4 == nil {
		t.Fatal("GetSized(1000) returned nil")
	}
	if buf4.Size() != 1000 { // Should get custom sized buffer
		t.Errorf("Expected buffer size 1000, got %d", buf4.Size())
	}

	// Test 5: Request larger than max (should cap at maxSize)
	buf5, err := pool.GetSized(5000)
	if err != nil {
		t.Fatalf("GetSized(5000) error: %v", err)
	}
	if buf5 == nil {
		t.Fatal("GetSized(5000) returned nil")
	}
	if buf5.Size() != 2000 { // Should be capped at maxSize
		t.Errorf("Expected buffer size 2000 (capped), got %d", buf5.Size())
	}

	// Test 6: Request smaller than minSize (enhanced pool clamps to minSize)
	buf6, err := pool.GetSized(50)
	if err != nil {
		t.Fatalf("GetSized(50) error: %v", err)
	}
	if buf6 == nil {
		t.Fatal("GetSized(50) returned nil")
	}
	if buf6.Size() != 100 { // Enhanced pool clamps to minSize
		t.Errorf("Expected buffer size 100 (clamped to minSize), got %d", buf6.Size())
	}

	// Clean up - return pool buffers
	pool.Put(buf1)
	pool.Put(buf2)
	pool.Put(buf3)
	// buf4, buf5, buf6 are custom sized and should not be returned to pool
}

// TestBufferPoolPutCustomSized tests that custom sized buffers are not returned to pool
func TestBufferPoolPutCustomSized(t *testing.T) {
	t.Parallel()

	pool := NewBufferPool(100, 500, 2000)

	// Get a custom sized buffer
	customBuf, err := pool.GetSized(1000)
	if err != nil {
		t.Fatalf("GetSized(1000) error: %v", err)
	}
	if customBuf.Size() != 1000 {
		t.Fatalf("Expected custom buffer size 1000, got %d", customBuf.Size())
	}

	// Put should not panic with custom sized buffer
	pool.Put(customBuf)

	// Put should handle nil buffer gracefully
	pool.Put(nil)
}

// TestComposeJobNewComposeJob tests the NewComposeJob() constructor that currently has 0% coverage
func TestComposeJobNewComposeJob(t *testing.T) {
	t.Parallel()

	job := NewComposeJob()
	if job == nil {
		t.Fatal("NewComposeJob() returned nil")
	}

	// The constructor just creates an empty job - defaults are set elsewhere
	// Test basic functionality
	job.Name = "test-compose"
	job.File = "docker-compose.yml"
	job.Service = "web"

	// Test that basic fields work
	if job.Name != "test-compose" {
		t.Errorf("Expected name 'test-compose', got %q", job.Name)
	}

	// Test that it can be used as a Job interface
	var _ Job = job
}

// TestResetMiddlewares tests the ResetMiddlewares function that currently has 0% coverage
func TestResetMiddlewares(t *testing.T) {
	t.Parallel()

	// Create a job that has middlewares
	job := &LocalJob{}

	// Add middleware - middlewares are deduplicated by type, so we can only have one TestMiddleware
	middleware1 := &TestMiddleware{}
	job.Use(middleware1)

	// Verify middleware was added
	middlewares := job.Middlewares()
	if len(middlewares) != 1 {
		t.Errorf("Expected 1 middleware after Use, got %d", len(middlewares))
	}

	// Reset middlewares with new ones
	middleware2 := &TestMiddleware{}
	job.ResetMiddlewares(middleware2)

	// Verify old middlewares were cleared and new one was added
	middlewares = job.Middlewares()
	if len(middlewares) != 1 {
		t.Errorf("Expected 1 middleware after ResetMiddlewares, got %d", len(middlewares))
	}

	if middlewares[0] != middleware2 {
		t.Error("ResetMiddlewares didn't set the correct middleware")
	}

	// Test reset with no middlewares - this is the main test since ResetMiddlewares clears all
	job.ResetMiddlewares()
	middlewares = job.Middlewares()
	if len(middlewares) != 0 {
		t.Errorf("Expected 0 middlewares after ResetMiddlewares(), got %d", len(middlewares))
	}
}
