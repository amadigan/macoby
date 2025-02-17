package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/amadigan/macoby/internal/applog"
)

func init() {
	buildPlan.tasks["railyard"] = task{
		name:    "railyard",
		outputs: []string{"bin/railyard"},
		run:     buildPlan.buildRailyard,
	}
}

func (p *plan) buildRailyard(ctx context.Context, t task, arch string) error {
	outfile := filepath.Join(p.outputDir, arch, t.outputs[0])

	var mtime time.Time

	if stat, err := os.Stat(outfile); err == nil {
		mtime = stat.ModTime()
	}

	target := "./cmd/railyard"

	args := []string{"build", "-o", outfile}
	args = append(args, p.gobuildargs...)
	args = append(args, target)

	cmd := exec.Command("go", args...)
	cmd.Dir = p.root

	cmd.Env = append(os.Environ(), "GOARCH="+arch, "GOOS=darwin", "CGO_ENABLED=1")

	if err := p.run(ctx, cmd); err != nil {
		logf(ctx, applog.LogLevelError, "failed to build %s: %v", target, err)

		return err
	}

	if stat, err := os.Stat(outfile); err == nil && !stat.ModTime().After(mtime) {
		logf(ctx, applog.LogLevelDebug, "no change to %s", outfile)

		return nil
	}

	codesigner := p.codesigner

	if codesigner == "" {
		codesigner = "-"
	}

	cmd = exec.Command("codesign", "--entitlements", "build/entitlements.plist", "--sign", codesigner, "--timestamp",
		"--options", "runtime", "--force", "--verbose", outfile)
	cmd.Dir = p.root

	if err := p.run(ctx, cmd); err != nil {
		logf(ctx, applog.LogLevelError, "failed to codesign %s: %v", outfile, err)

		return err
	}

	return nil
}
