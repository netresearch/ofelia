// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
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
	Logger    *slog.Logger
	Job       Job
	Execution *Execution
	Ctx       context.Context //nolint:containedctx // intentional: propagates go-cron's per-entry context through middleware chain

	current     int
	executed    bool
	middlewares []Middleware
}

// NewContext constructs a Context with a non-nil default Ctx
// (context.TODO()). Prefer NewContextWithContext to thread the scheduler's
// per-run, deadline-bounded context through the middleware chain;
// NewContext is kept for legacy callers that have no parent context yet.
//
// The default is context.TODO (not context.Background) to flag — both to
// readers and to static analyzers — that the caller still owes a real
// parent context. See issue #638.
func NewContext(s *Scheduler, j Job, e *Execution) *Context {
	return NewContextWithContext(context.TODO(), s, j, e)
}

// NewContextWithContext creates a Context with a specific context.Context,
// typically the per-entry, deadline-bounded context produced by the
// scheduler around each invocation. The provided ctx must be non-nil; a
// nil ctx is replaced with context.TODO() so downstream RunContext()
// always returns a usable context.
//
//nolint:contextcheck // intentional: only substitutes context.TODO() when caller passes nil; non-nil ctx is stored unchanged
func NewContextWithContext(ctx context.Context, s *Scheduler, j Job, e *Execution) *Context {
	if ctx == nil {
		ctx = context.TODO()
	}
	return &Context{
		Scheduler:   s,
		Logger:      s.Logger,
		Job:         j,
		Execution:   e,
		Ctx:         ctx,
		middlewares: j.Middlewares(),
	}
}

// RunContext returns the context.Context that job Run methods should use
// for outbound calls (Docker SDK, env-from inspections, etc.). It returns
// c.Ctx when set; otherwise context.Background() so legacy *Context{}
// literals (e.g. in tests that predate context plumbing) keep working.
//
// Centralizing the fallback here is the foundation for issue #638: it
// replaces four inline `if runCtx == nil { runCtx = context.Background() }`
// blocks (execjob, runjob, runservice, localjob) with a single call site.
func (c *Context) RunContext() context.Context {
	if c == nil || c.Ctx == nil {
		return context.Background()
	}
	return c.Ctx
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
	formatted := fmt.Sprintf(logPrefix, c.Job.GetName(), c.Execution.ID, msg)

	switch {
	case c.Execution.Failed:
		c.Logger.Error(formatted)
	case c.Execution.Skipped:
		c.Logger.Warn(formatted)
	default:
		c.Logger.Info(formatted)
	}
}

func (c *Context) Warn(msg string) {
	formatted := fmt.Sprintf(logPrefix, c.Job.GetName(), c.Execution.ID, msg)
	c.Logger.Warn(formatted)
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

// keyedMiddleware is an OPTIONAL interface that middleware implementations
// can satisfy to opt into per-instance dedup keys in middlewareContainer.
// Implementers that legitimately need multiple instances per job (e.g.
// multiple Webhook destinations with distinct Config.Name) return a stable,
// non-empty key per instance — typically the operator-supplied name from
// configuration. Returning "" or not implementing Key() at all falls back
// to the legacy reflect.TypeOf(m).String() dedup, preserving the
// 1-per-type semantics of Slack, Mail, Save, Overlap, and the
// WebhookMiddleware composite from PR #671.
//
// Why opt-in via type assertion (no interface contract change): adding
// Key() to the Middleware interface would force every implementer (in
// this repo and any downstream consumer) to add a stub method, providing
// no value for the common 1-per-type case. The assertion-based opt-in
// keeps the contract small and lets the type-string fallback do the work
// it has always done.
//
// Tracks the structural fix for
// https://github.com/netresearch/ofelia/issues/672; sibling to #670 /
// PR #671 which worked around the multi-webhook case at the webhook layer.
type keyedMiddleware interface {
	Key() string
}

// middlewareKey computes the dedup key for a middleware. Implementers of
// keyedMiddleware that return a non-empty string get that string as the
// key; everyone else (including keyedMiddleware implementers that
// explicitly return "") falls back to reflect.TypeOf(m).String().
func middlewareKey(m Middleware) string {
	if k, ok := m.(keyedMiddleware); ok {
		if key := k.Key(); key != "" {
			return key
		}
	}
	return reflect.TypeOf(m).String()
}

// middlewareContainer holds a job's (or scheduler's) middleware chain with
// per-key dedup so the same logical middleware is not invoked twice when
// scheduler-level middlewares are propagated to a job via
// j.Use(s.Middlewares()...). See middlewareKey for the dedup-key strategy.
//
// Not safe for concurrent use; callers must serialize. In practice
// (*Scheduler).Use happens under Scheduler.mu, and (*BareJob).Use is
// invoked from config-parse paths and from AddJob before the job is
// published to the cron runner — there is no in-flight reader during
// mutation.
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

		k := middlewareKey(m)
		if _, ok := c.m[k]; ok {
			continue
		}

		c.order = append(c.order, k)
		c.m[k] = m
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

func randomID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand read: %w", err)
	}

	return fmt.Sprintf("%x", b), nil
}

const HashmeTagName = "hash"

// hashmeTagEnabled is the struct tag value that enables hashing for a field.
const hashmeTagEnabled = "true"

func GetHash(t reflect.Type, v reflect.Value, hash *string) error {
	for field := range t.Fields() {
		fieldv := v.FieldByIndex(field.Index)

		if field.Type.Kind() == reflect.Struct && field.Type != reflect.TypeFor[time.Duration]() {
			if err := GetHash(field.Type, fieldv, hash); err != nil {
				return err
			}
			continue
		}

		if field.Tag.Get(HashmeTagName) != hashmeTagEnabled {
			continue
		}

		if err := appendFieldHash(field, fieldv, hash); err != nil {
			return err
		}
	}

	return nil
}

// appendFieldHash appends the stable string representation of a single
// hashme-tagged field to hash, returning ErrUnsupportedFieldType for kinds
// that are not part of the job hash.
func appendFieldHash(field reflect.StructField, fieldv reflect.Value, hash *string) error {
	//nolint:exhaustive // reflect.Kind has many values; only relevant kinds are supported for hashing
	switch field.Type.Kind() {
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
		for i := range fieldv.Len() {
			str := fieldv.Index(i).String()
			*hash += fmt.Sprintf("%d:%s,", len(str), str)
		}
	case reflect.Pointer:
		if fieldv.IsNil() {
			*hash += "<nil>"
			return nil
		}
		elem := fieldv.Elem()
		if elem.Kind() == reflect.String {
			*hash += elem.String()
			return nil
		}
		return fmt.Errorf("%w: field '%s' of type '%s'", ErrUnsupportedFieldType, field.Name, field.Type)
	// Other kinds are intentionally not part of the job hash. They are either
	// not used in our job structs today or would require a more elaborate
	// stable string representation that is out of scope here.
	default:
		return fmt.Errorf("%w: field '%s' of type '%s'", ErrUnsupportedFieldType, field.Name, field.Type)
	}

	return nil
}
