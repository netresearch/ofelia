package core

import (
	"sync"

	"github.com/armon/circbuf"
)

// BufferPool manages a pool of reusable circular buffers to reduce memory allocation
type BufferPool struct {
	pool    sync.Pool
	size    int64
	maxSize int64
	minSize int64
}

// NewBufferPool creates a new buffer pool with configurable sizes
func NewBufferPool(minSize, defaultSize, maxSize int64) *BufferPool {
	bp := &BufferPool{
		size:    defaultSize,
		maxSize: maxSize,
		minSize: minSize,
	}

	bp.pool = sync.Pool{
		New: func() interface{} {
			// Create a new buffer with default size
			buf, _ := circbuf.NewBuffer(bp.size)
			return buf
		},
	}

	return bp
}

// Get retrieves a buffer from the pool or creates a new one
func (bp *BufferPool) Get() *circbuf.Buffer {
	return bp.pool.Get().(*circbuf.Buffer)
}

// GetSized retrieves a buffer with a specific size requirement
func (bp *BufferPool) GetSized(size int64) *circbuf.Buffer {
	// If requested size is within our normal range, use the pool
	if size >= bp.minSize && size <= bp.size {
		return bp.Get()
	}

	// Otherwise create a custom-sized buffer
	// Cap at maxSize to prevent excessive memory usage
	if size > bp.maxSize {
		size = bp.maxSize
	}

	buf, _ := circbuf.NewBuffer(size)
	return buf
}

// Put returns a buffer to the pool for reuse
func (bp *BufferPool) Put(buf *circbuf.Buffer) {
	if buf == nil {
		return
	}

	// Reset the buffer before returning to pool
	buf.Reset()

	// Only return to pool if it's the standard size
	// Custom-sized buffers are let go for GC
	if buf.Size() == bp.size {
		bp.pool.Put(buf)
	}
}

// Global buffer pool with sensible defaults
var (
	// DefaultBufferPool provides buffers for job execution
	// Min: 1KB for tiny outputs
	// Default: 256KB for typical outputs
	// Max: 10MB for large outputs (matching current maxStreamSize)
	DefaultBufferPool = NewBufferPool(1024, 256*1024, maxStreamSize)
)
