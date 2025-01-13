package applog

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync/atomic"
	"time"
)

type LogLevel uint8

const (
	LogLevelOff LogLevel = iota
	LogLevelDebug
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelFatal
)

var logLevelNames = []string{"OFF", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"}

var minLogLevel atomic.Int32

func (l LogLevel) String() string {
	if l < LogLevelOff || l > LogLevelFatal {
		return ""
	}

	return logLevelNames[l]
}

type Logger struct {
	pkg string
}

var logger = log.New(os.Stderr, "", 0)

func Log(level LogLevel, when time.Time, pkg string, msg string, args ...any) {
	if minLogLevel.Load() > int32(level) {
		return
	}

	nargs := make([]any, 3, len(args)+3)
	nargs[0] = when.UTC().Format(time.RFC3339)
	nargs[1] = level.String()
	nargs[2] = pkg

	nargs = append(nargs, args...)

	logger.Printf("%s %s %s: "+msg, nargs...)
}

func SetOutput(w io.Writer) {
	logger.SetOutput(w)
}

func New(pkg string) *Logger {
	return &Logger{pkg: pkg}
}

func (l *Logger) Debug(msg string) {
	Log(LogLevelDebug, time.Now(), l.pkg, "%s", msg)
}

func (l *Logger) Debugf(msg string, args ...any) {
	Log(LogLevelDebug, time.Now(), l.pkg, msg, args...)
}

func (l *Logger) Info(msg string) {
	Log(LogLevelInfo, time.Now(), l.pkg, "%s", msg)
}

func (l *Logger) Infof(msg string, args ...any) {
	Log(LogLevelInfo, time.Now(), l.pkg, msg, args...)
}

func (l *Logger) Warn(msg string) {
	Log(LogLevelWarn, time.Now(), l.pkg, "%s", msg)
}

func (l *Logger) Warnf(msg string, args ...any) {
	Log(LogLevelWarn, time.Now(), l.pkg, msg, args...)
}

func (l *Logger) Error(msg string) {
	Log(LogLevelError, time.Now(), l.pkg, "%s", msg)
}

func (l *Logger) Errorf(msg string, args ...any) {
	Log(LogLevelError, time.Now(), l.pkg, msg, args...)
}

func (l *Logger) Fatal(err error) {
	Log(LogLevelFatal, time.Now(), l.pkg, "%v", err)
	panic(err)
}

func (l *Logger) Fatalf(msg string, args ...any) {
	Log(LogLevelFatal, time.Now(), l.pkg, msg, args...)
	panic(fmt.Sprintf(msg, args...))
}
