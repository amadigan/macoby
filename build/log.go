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

func logLoop() {
	for msg := range buildPlan.logchan {
		msgtime := msg.time.Truncate(time.Second).UTC().Format(time.RFC3339)
		fmt.Printf("[%s] %s %s %s\n", msgtime, msg.level, msg.task, msg.message)
	}
}

func logf(ctx context.Context, level applog.LogLevel, format string, args ...any) {
	if logLevel > level {
		return
	}

	btask, _ := ctx.Value(ctxkeyTask).(task)
	task := btask.name

	if task == "" {
		task = "global"
	}

	if arch, ok := ctx.Value(ctykeyArch).(string); ok {
		task += "-" + arch
	}

	buildPlan.logchan <- logMessage{
		time:    time.Now(),
		level:   level,
		task:    task,
		message: fmt.Sprintf(format, args...),
	}
}
