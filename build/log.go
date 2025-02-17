package main

import (
	"context"
	"fmt"
	"time"

	"github.com/amadigan/macoby/internal/applog"
)

var logLevel = applog.LogLevelInfo

type logMessage struct {
	time    time.Time
	level   applog.LogLevel
	task    string
	message string
}

const logFormat = "[%s] %s %s %s\n"

type logchanKey struct{}

var logchanCtxKey = logchanKey{}

func logLoop() {
	for msg := range buildPlan.logchan {
		msgtime := msg.time.Truncate(time.Second).UTC().Format(time.RFC3339)
		fmt.Printf(logFormat, msgtime, msg.level, msg.task, msg.message) //nolint:forbidigo
	}
}

func logContext(ctx context.Context) string {
	btask, _ := ctx.Value(ctxkeyTask).(task)
	task := btask.name

	if task == "" {
		task = "global"
	}

	if arch, ok := ctx.Value(ctykeyArch).(string); ok {
		task += "-" + arch
	}

	return task
}

func redirectLogs(ctx context.Context, ch chan logMessage) context.Context {
	return context.WithValue(ctx, logchanCtxKey, ch)
}

func logf(ctx context.Context, level applog.LogLevel, format string, args ...any) {
	if logLevel > level {
		return
	}

	logtime := time.Now()
	msg := fmt.Sprintf(format, args...)

	if ch, ok := ctx.Value(logchanCtxKey).(chan logMessage); ok {
		ch <- logMessage{
			time:    logtime,
			level:   level,
			task:    logContext(ctx),
			message: msg,
		}
	} else {
		fmt.Printf(logFormat, time.Now().UTC().Format(time.RFC3339), level, logContext(ctx), msg) //nolint:forbidigo
	}
}
