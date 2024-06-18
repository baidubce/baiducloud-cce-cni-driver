package log

var _ SDKLogger = &logger{}

type SDKLogger interface {
	Logging(level Level, format string, args ...interface{})
}

func (l *logger) Logging(level Level, format string, args ...interface{}) {
	l.logging(level, format, args...)
}

func SetLogger(logger SDKLogger) {
	gDefaultLogger = logger
}
