package core

import (
	"sync/atomic"
	"time"
)

// Test middleware and logger types used by gocheck-based tests in this package

type TestMiddleware struct {
	called atomic.Int32
}

func (m *TestMiddleware) ContinueOnStop() bool   { return false }
func (m *TestMiddleware) Run(ctx *Context) error { m.called.Add(1); return nil }
func (m *TestMiddleware) Called() int            { return int(m.called.Load()) }

type TestJob struct {
	BareJob
	called atomic.Int32
}

func (j *TestJob) Run(ctx *Context) error {
	j.called.Add(1)
	time.Sleep(time.Millisecond * 50)
	return nil
}

func (j *TestJob) Called() int { return int(j.called.Load()) }

type TestLogger struct{}

func (*TestLogger) Criticalf(string, ...any) {}
func (*TestLogger) Debugf(string, ...any)    {}
func (*TestLogger) Errorf(string, ...any)    {}
func (*TestLogger) Noticef(string, ...any)   {}
func (*TestLogger) Warningf(string, ...any)  {}
