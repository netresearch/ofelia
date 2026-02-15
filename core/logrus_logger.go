package core

import (
	"runtime"
	"sync"

	"github.com/sirupsen/logrus"
)

// LogrusAdapter wraps a logrus.Logger to satisfy the Logger interface.
type LogrusAdapter struct {
	*logrus.Logger
	mu sync.Mutex // Protects ReportCaller modifications
}

var _ Logger = (*LogrusAdapter)(nil)

func (l *LogrusAdapter) logf(level logrus.Level, format string, args ...any) {
	var frame *runtime.Frame
	if pc, file, line, ok := runtime.Caller(2); ok {
		frame = &runtime.Frame{PC: pc, File: file, Line: line, Function: runtime.FuncForPC(pc).Name()}
	}

	l.mu.Lock()
	prev := l.Logger.ReportCaller
	l.Logger.ReportCaller = false
	defer func() {
		l.Logger.ReportCaller = prev
		l.mu.Unlock()
	}()

	entry := logrus.NewEntry(l.Logger)
	entry.Caller = frame
	entry.Logf(level, format, args...)
}

func (l *LogrusAdapter) Criticalf(format string, args ...any) {
	l.logf(logrus.FatalLevel, format, args...)
}

func (l *LogrusAdapter) Debugf(format string, args ...any) {
	l.logf(logrus.DebugLevel, format, args...)
}

func (l *LogrusAdapter) Errorf(format string, args ...any) {
	l.logf(logrus.ErrorLevel, format, args...)
}

func (l *LogrusAdapter) Noticef(format string, args ...any) {
	l.logf(logrus.InfoLevel, format, args...)
}

func (l *LogrusAdapter) Warningf(format string, args ...any) {
	l.logf(logrus.WarnLevel, format, args...)
}
