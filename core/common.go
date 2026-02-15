package core

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/armon/circbuf"
)

// ErrSkippedExecution pass this error to `Execution.Stop` if you wish to mark
// it as skipped.
var ErrSkippedExecution = errors.New("skipped execution")

const (
	// maximum size of a stdout/stderr stream to be kept in memory and optional stored/sent via mail
	maxStreamSize = 10 * 1024 * 1024
	logPrefix     = "[Job %q (%s)] %s"
)

type Job interface {
	GetName() string
	GetSchedule() string
	GetCommand() string
	ShouldRunOnStartup() bool
	Middlewares() []Middleware
	Use(...Middleware)
	Run(*Context) error
	Running() int32
	NotifyStart()
	NotifyStop()
	GetCronJobID() uint64
	SetCronJobID(uint64)
	GetHistory() []*Execution
	Hash() (string, error)
}

type Context struct {
	Scheduler *Scheduler
	Logger    Logger
	Job       Job
	Execution *Execution
	Ctx       context.Context //nolint:containedctx // intentional: propagates go-cron's per-entry context through middleware chain

	current     int
	executed    bool
	middlewares []Middleware
}

func NewContext(s *Scheduler, j Job, e *Execution) *Context {
	return &Context{
		Scheduler:   s,
		Logger:      s.Logger,
		Job:         j,
		Execution:   e,
		Ctx:         context.Background(),
		middlewares: j.Middlewares(),
	}
}

// NewContextWithContext creates a Context with a specific context.Context,
// typically the per-entry context provided by go-cron's JobWithContext interface.
func NewContextWithContext(ctx context.Context, s *Scheduler, j Job, e *Execution) *Context {
	return &Context{
		Scheduler:   s,
		Logger:      s.Logger,
		Job:         j,
		Execution:   e,
		Ctx:         ctx,
		middlewares: j.Middlewares(),
	}
}

func (c *Context) Start() {
	c.Execution.Start()
	c.Job.NotifyStart()
}

func (c *Context) Next() error {
	if err := c.doNext(); err != nil || c.executed {
		c.Stop(err)
	}

	return nil
}

func (c *Context) doNext() error {
	for {
		m, end := c.getNext()
		if end {
			break
		}

		if !c.Execution.IsRunning && !m.ContinueOnStop() {
			continue
		}

		if err := m.Run(c); err != nil {
			return fmt.Errorf("middleware run: %w", err)
		}
		return nil
	}

	if !c.Execution.IsRunning {
		return nil
	}

	c.executed = true
	if err := c.Job.Run(c); err != nil {
		return fmt.Errorf("job run: %w", err)
	}
	return nil
}

func (c *Context) getNext() (Middleware, bool) {
	if c.current >= len(c.middlewares) {
		return nil, true
	}

	c.current++
	return c.middlewares[c.current-1], false
}

func (c *Context) Stop(err error) {
	if !c.Execution.IsRunning {
		return
	}

	c.Execution.Stop(err)
	c.Job.NotifyStop()
}

func (c *Context) Log(msg string) {
	args := []any{c.Job.GetName(), c.Execution.ID, msg}

	switch {
	case c.Execution.Failed:
		c.Logger.Errorf(logPrefix, args...)
	case c.Execution.Skipped:
		c.Logger.Warningf(logPrefix, args...)
	default:
		c.Logger.Noticef(logPrefix, args...)
	}
}

func (c *Context) Warn(msg string) {
	args := []any{c.Job.GetName(), c.Execution.ID, msg}
	c.Logger.Warningf(logPrefix, args...)
}

// Execution contains all the information relative to a Job execution.
type Execution struct {
	ID        string
	Date      time.Time
	Duration  time.Duration
	IsRunning bool
	Failed    bool
	Skipped   bool
	Error     error

	OutputStream, ErrorStream *circbuf.Buffer `json:"-"`

	// Captured output for persistence after buffer cleanup
	CapturedStdout, CapturedStderr string `json:"-"`
}

// NewExecution returns a new Execution, with a random ID
func NewExecution() (*Execution, error) {
	// Use buffer pool to reduce memory allocation
	bufOut, err := DefaultBufferPool.Get()
	if err != nil {
		return nil, fmt.Errorf("failed to get output buffer: %w", err)
	}

	bufErr, err := DefaultBufferPool.Get()
	if err != nil {
		DefaultBufferPool.Put(bufOut) // Return already-allocated buffer
		return nil, fmt.Errorf("failed to get error buffer: %w", err)
	}

	id, err := randomID()
	if err != nil {
		// Return buffers to pool on error
		DefaultBufferPool.Put(bufOut)
		DefaultBufferPool.Put(bufErr)
		return nil, err
	}
	return &Execution{
		ID:           id,
		OutputStream: bufOut,
		ErrorStream:  bufErr,
	}, nil
}

// Start starts the execution, initializes the running flags and the start date.
func (e *Execution) Start() {
	e.IsRunning = true
	e.Date = time.Now()
}

// Stop halts the execution. If a ErrSkippedExecution is given the execution
// is marked as skipped; if any other error is given the execution is marked as
// failed. Also mark the execution as IsRunning false and save the duration time
func (e *Execution) Stop(err error) {
	e.IsRunning = false
	// Guard against zero or unset start time and ensure a positive duration
	if e.Date.IsZero() {
		e.Date = time.Now()
	}
	e.Duration = time.Since(e.Date)
	if e.Duration <= 0 {
		e.Duration = time.Nanosecond
	}

	if err != nil && !errors.Is(err, ErrSkippedExecution) {
		e.Error = err
		e.Failed = true
	} else if errors.Is(err, ErrSkippedExecution) {
		e.Skipped = true
	}
}

// GetStdout returns stdout content, preferring live buffer if available
func (e *Execution) GetStdout() string {
	if e.OutputStream != nil {
		return e.OutputStream.String()
	}
	return e.CapturedStdout
}

// GetStderr returns stderr content, preferring live buffer if available
func (e *Execution) GetStderr() string {
	if e.ErrorStream != nil {
		return e.ErrorStream.String()
	}
	return e.CapturedStderr
}

// Cleanup returns execution buffers to the pool for reuse
func (e *Execution) Cleanup() {
	// Capture buffer contents before cleanup for persistence
	if e.OutputStream != nil {
		e.CapturedStdout = e.OutputStream.String()
		DefaultBufferPool.Put(e.OutputStream)
		e.OutputStream = nil
	}
	if e.ErrorStream != nil {
		e.CapturedStderr = e.ErrorStream.String()
		DefaultBufferPool.Put(e.ErrorStream)
		e.ErrorStream = nil
	}
}

// Middleware can wrap any job execution, allowing to execution code before
// or/and after of each `Job.Run`
type Middleware interface {
	// Run is called instead of the original `Job.Run`, you MUST call to `ctx.Run`
	// inside of the middleware `Run` function otherwise you will broken the
	// Job workflow.
	Run(*Context) error
	// ContinueOnStop reports whether Run should be called even when the
	// execution has been stopped
	ContinueOnStop() bool
}

type middlewareContainer struct {
	m     map[string]Middleware
	order []string
}

func (c *middlewareContainer) Use(ms ...Middleware) {
	if c.m == nil {
		c.m = make(map[string]Middleware, 0)
	}

	for _, m := range ms {
		if m == nil {
			continue
		}

		t := reflect.TypeOf(m).String()
		if _, ok := c.m[t]; ok {
			continue
		}

		c.order = append(c.order, t)
		c.m[t] = m
	}
}

func (c *middlewareContainer) ResetMiddlewares(ms ...Middleware) {
	c.m = nil
	c.order = nil
	c.Use(ms...)
}

func (c *middlewareContainer) Middlewares() []Middleware {
	ms := make([]Middleware, 0, len(c.order))
	for _, t := range c.order {
		ms = append(ms, c.m[t])
	}
	return ms
}

type Logger interface {
	Criticalf(format string, args ...any)
	Debugf(format string, args ...any)
	Errorf(format string, args ...any)
	Noticef(format string, args ...any)
	Warningf(format string, args ...any)
}

func randomID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand read: %w", err)
	}

	return fmt.Sprintf("%x", b), nil
}

const HashmeTagName = "hash"

func GetHash(t reflect.Type, v reflect.Value, hash *string) error {
	for field := range t.Fields() {
		fieldv := v.FieldByIndex(field.Index)
		kind := field.Type.Kind()

		if kind == reflect.Struct && field.Type != reflect.TypeFor[time.Duration]() {
			if err := GetHash(field.Type, fieldv, hash); err != nil {
				return err
			}
			continue
		}

		hashmeTag := field.Tag.Get(HashmeTagName)
		if hashmeTag != "true" {
			continue
		}

		//nolint:exhaustive // reflect.Kind has many values; only relevant kinds are supported for hashing
		switch kind {
		case reflect.String:
			*hash += fieldv.String()
		case reflect.Int32, reflect.Int, reflect.Int64, reflect.Int16, reflect.Int8:
			*hash += strconv.FormatInt(fieldv.Int(), 10)
		case reflect.Bool:
			*hash += strconv.FormatBool(fieldv.Bool())
		case reflect.Slice:
			if field.Type.Elem().Kind() != reflect.String {
				return ErrUnsupportedFieldType
			}
			strs := fieldv.Interface().([]string)
			for _, str := range strs {
				*hash += fmt.Sprintf("%d:%s,", len(str), str)
			}
		case reflect.Pointer:
			if fieldv.IsNil() {
				*hash += "<nil>"
				continue
			}
			elem := fieldv.Elem()
			if elem.Kind() == reflect.String {
				*hash += elem.String()
				continue
			}
			return fmt.Errorf("%w: field '%s' of type '%s'", ErrUnsupportedFieldType, field.Name, field.Type)
		// Other kinds are intentionally not part of the job hash. They are either
		// not used in our job structs today or would require a more elaborate
		// stable string representation that is out of scope here.
		default:
			return fmt.Errorf("%w: field '%s' of type '%s'", ErrUnsupportedFieldType, field.Name, field.Type)
		}
	}

	return nil
}
