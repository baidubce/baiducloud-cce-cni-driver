package cloud

import (
	sdklog "github.com/baidubce/bce-sdk-go/util/log"
	"github.com/sirupsen/logrus"
)

var _ sdklog.SDKLogger = &bceLogger{}

type bceLogger struct{}

// Logging implements log.SDKLogger.
func (*bceLogger) Logging(level sdklog.Level, format string, args ...interface{}) {
	// convert log level to logrus level
	var logrusLevel logrus.Level
	switch level {
	case sdklog.DEBUG:
		logrusLevel = logrus.DebugLevel
	case sdklog.INFO:
		logrusLevel = logrus.InfoLevel
	case sdklog.WARN:
		logrusLevel = logrus.WarnLevel
	case sdklog.ERROR:
		logrusLevel = logrus.ErrorLevel
	case sdklog.FATAL:
		logrusLevel = logrus.FatalLevel
	case sdklog.PANIC:
		logrusLevel = logrus.PanicLevel
	default:
		logrusLevel = logrus.TraceLevel
	}
	log.Logf(logrusLevel, format, args...)
}
