package core

import "time"

// Test middleware and logger types used by gocheck-based tests in this package

type TestMiddleware struct {
	Called int
}

func (m *TestMiddleware) ContinueOnStop() bool   { return false }
func (m *TestMiddleware) Run(ctx *Context) error { m.Called++; return nil }

type TestJob struct {
	BareJob
	Called int
}

func (j *TestJob) Run(ctx *Context) error {
	j.Called++
	time.Sleep(time.Millisecond * 50)
	return nil
}

type TestLogger struct{}

func (*TestLogger) Criticalf(string, ...interface{}) {}
func (*TestLogger) Debugf(string, ...interface{})    {}
func (*TestLogger) Errorf(string, ...interface{})    {}
func (*TestLogger) Noticef(string, ...interface{})   {}
func (*TestLogger) Warningf(string, ...interface{})  {}
