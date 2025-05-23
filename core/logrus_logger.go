package core

import "github.com/sirupsen/logrus"

// LogrusAdapter wraps a logrus.Logger to satisfy the Logger interface.
type LogrusAdapter struct {
	*logrus.Logger
}

var _ Logger = (*LogrusAdapter)(nil)

func (l *LogrusAdapter) Criticalf(format string, args ...interface{}) {
	l.Logger.Logf(logrus.FatalLevel, format, args...)
}

func (l *LogrusAdapter) Debugf(format string, args ...interface{}) {
	l.Logger.Debugf(format, args...)
}

func (l *LogrusAdapter) Errorf(format string, args ...interface{}) {
	l.Logger.Errorf(format, args...)
}

func (l *LogrusAdapter) Noticef(format string, args ...interface{}) {
	l.Logger.Infof(format, args...)
}

func (l *LogrusAdapter) Warningf(format string, args ...interface{}) {
	l.Logger.Warnf(format, args...)
}
