package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sync"
	"time"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/util"
)

type task struct {
	name    string
	inputs  []string
	outputs []string
	bake    bool
	run     func(context.Context, task, string) error
}

type ctxkey string

const (
	ctxkeyTask = ctxkey("task")
	ctykeyArch = ctxkey("arch")
)

type plan struct {
	root      string
	outputDir string
	tasks     map[string]task // all possible tasks
	bakeChan  chan struct{}   // signal when bake is done

	dryrun         bool
	targets        map[string]util.Set[string] // map of tasks to list of archs
	bakevars       map[string]string
	gobuildargs    []string
	maxCompression uint16

	codesigner string
	pkgsigner  string

	outputs   map[string]chan struct{} // closes when the output is ready
	processes map[string]*exec.Cmd
	logchan   chan logMessage

	err   error
	mutex sync.Mutex
}

var buildPlan = &plan{
	tasks:          map[string]task{},
	bakeChan:       make(chan struct{}),
	outputs:        map[string]chan struct{}{},
	processes:      map[string]*exec.Cmd{},
	targets:        map[string]util.Set[string]{},
	maxCompression: zip.Deflate,
	bakevars:       map[string]string{},
	gobuildargs:    []string{"-mod=mod"},
	logchan:        make(chan logMessage, 100),
}

func (p *plan) checkError() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	return p.err
}

func (p *plan) setError(err error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.err = err
}

func (p *plan) prepare(ctx context.Context, reuse bool, targets []string, archs []string) error {
	outputs := make(map[string]string, len(p.tasks))

	for name, task := range p.tasks {
		for _, output := range task.outputs {
			outputs[output] = name
		}
	}

	p.add(targets, archs)

	changed := true
	for iter := 0; changed; iter++ {
		if iter > 10 {
			return fmt.Errorf("too many iterations")
		}

		changed = false

		for _, task := range targets {
			for _, input := range p.tasks[task].inputs {
				updated, err := p.prepareOutput(ctx, reuse, input, outputs[input], archs)
				if err != nil {
					return err
				}

				if updated {
					changed = true
				}
			}
		}
	}

	return nil
}

func (p *plan) prepareOutput(ctx context.Context, reuse bool, output string, task string, archs []string) (bool, error) {
	changed := false

	for _, arch := range archs {
		if !p.hasOutput(arch, output) {
			updated, err := p.planOutput(ctx, arch, output, reuse, task)
			if err != nil {
				return false, err
			}

			if updated {
				changed = true
			}
		}
	}

	return changed, nil
}

func (p *plan) planOutput(ctx context.Context, arch string, name string, reuse bool, task string) (bool, error) {
	outname := path.Join(arch, name)
	logf(ctx, applog.LogLevelInfo, "planning %s with task %s", outname, task)

	if reuse {
		logf(ctx, applog.LogLevelInfo, "checking %s", outname)

		if stat, err := os.Stat(path.Join(p.outputDir, outname)); err == nil && stat.Mode().IsRegular() {
			logf(ctx, applog.LogLevelInfo, "skipping %s", outname)

			return false, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("checking %s: %w", outname, err)
		} else if err != nil {
			logf(ctx, applog.LogLevelInfo, "missing %s", outname)
		} else {
			logf(ctx, applog.LogLevelInfo, "not a file %s", outname)
		}
	}

	if task == "" {
		return false, fmt.Errorf("missing task for %s", outname)
	}

	p.add([]string{task}, []string{arch})

	return true, nil
}

func (p *plan) add(tasks []string, archs []string) {
	for _, task := range tasks {
		tarTask := p.targets[task]
		if tarTask == nil {
			tarTask = util.NewSet(archs...)
			p.targets[task] = tarTask
		} else {
			tarTask.AddAll(archs...)
		}

		for _, output := range p.tasks[task].outputs {
			for _, arch := range archs {
				p.outputs[path.Join(arch, output)] = make(chan struct{})
			}
		}
	}
}

func (p *plan) hasOutput(arch, input string) bool {
	_, ok := p.outputs[path.Join(arch, input)]

	return ok
}

func (p *plan) wait(arch, input string) error {
	if ch, ok := p.outputs[path.Join(arch, input)]; !ok {
		panic(fmt.Sprintf("missing output channel for %s/%s", arch, input))
	} else if ch != nil {
		<-ch

		return p.checkError()
	}

	// nil channel means the output is already ready
	return nil
}

func (p *plan) run(ctx context.Context, cmd *exec.Cmd) error {
	task := ctx.Value(ctxkeyTask).(task).name
	if arch := ctx.Value(ctykeyArch).(string); arch != "" {
		task += "-" + arch
	}

	logf(ctx, applog.LogLevelInfo, "running %s", cmd.String())
	var out bytes.Buffer

	cmd.Stdout = &out
	cmd.Stderr = &out

	start := time.Now()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error running %s: %w", cmd.String(), err)
	}

	p.mutex.Lock()
	p.processes[task] = cmd
	p.mutex.Unlock()

	err := cmd.Wait()
	duration := time.Since(start)

	p.mutex.Lock()
	delete(p.processes, task)
	p.mutex.Unlock()

	var exitErr *exec.ExitError

	outstr := out.String()

	if outstr != "" {
		outstr = "\n" + outstr
	}

	if errors.As(err, &exitErr) {
		logf(ctx, applog.LogLevelInfo, "%s exited with code %d in %s%s", cmd.String(), exitErr.ExitCode(), duration, outstr)
	} else if err != nil {
		logf(ctx, applog.LogLevelError, "%s failed after %s: %v%s", cmd.String(), duration, err, outstr)
	} else {
		logf(ctx, applog.LogLevelInfo, "%s done in %s%s", cmd.String(), duration, outstr)
	}

	return err //nolint:wrapcheck
}

func (p *plan) shutdown(ctx context.Context) {
	for _, proc := range p.processes {
		if proc.ProcessState == nil {
			logf(ctx, applog.LogLevelInfo, "sending interrupt to %s", proc)
			_ = proc.Process.Signal(os.Interrupt)
		}
	}

	doneCh := make(chan struct{})

	go func() {
		for _, proc := range p.processes {
			if proc.ProcessState == nil {
				_, _ = proc.Process.Wait()
			}
		}
	}()

	select {
	case <-doneCh:
	case <-time.After(3 * time.Second):
	}

	for _, proc := range p.processes {
		if proc.ProcessState == nil {
			logf(ctx, applog.LogLevelInfo, "sending kill to %s", proc)
			_ = proc.Process.Kill()
		}
	}
}
