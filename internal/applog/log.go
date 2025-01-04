package log

import (
	"fmt"
	"sync"
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

func (l LogLevel) String() string {
	if l < LogLevelOff || l > LogLevelFatal {
		return ""
	}

	return logLevelNames[l]
}

type Logger struct {
	pkg string
}

type LogHandler interface {
	Log(LogLevel, time.Time, string, string, ...any)
}

type DefaultLogHandler struct {
	level LogLevel
}

func (h *DefaultLogHandler) Log(level LogLevel, when time.Time, pkg string, msg string, args ...any) {
	if level < h.level {
		return
	}

	nargs := make([]any, 3, len(args)+3)
	nargs[0] = when.Format(time.RFC3339)
	nargs[1] = level.String()
	nargs[2] = pkg

	nargs = append(nargs, args...)

	fmt.Printf("%s [%s] %s "+msg+"\n", nargs...)
}

var logHandler LogHandler = &DefaultLogHandler{}
var logMutex sync.RWMutex

func SetLogHandler(h LogHandler) {
	logMutex.Lock()
	defer logMutex.Unlock()
	logHandler = h
}

func New(pkg string) *Logger {
	return &Logger{pkg: pkg}
}

func Log(level LogLevel, when time.Time, pkg string, msg string, args ...any) {
	logMutex.RLock()
	defer logMutex.RUnlock()

	logHandler.Log(level, when, pkg, msg, args...)
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

func (l *Logger) Fatal(msg string) {
	Log(LogLevelFatal, time.Now(), l.pkg, "%s", msg)
}

func (l *Logger) Fatalf(msg string, args ...any) {
	Log(LogLevelFatal, time.Now(), l.pkg, msg, args...)
}
