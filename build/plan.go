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

var tasks = map[string]task{}

type plan struct {
	root      string
	outputDir string
	tasks     map[string]task // all possible tasks
	bakeChan  chan struct{}   // signal when bake is done

	dryrun         bool
	targets        map[string]map[string]struct{} // map of tasks to list of archs
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
	targets:        map[string]map[string]struct{}{},
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

func (p *plan) prepare(reuse bool, targets []string, archs []string) {
	outputs := make(map[string]string, len(p.tasks))

	for name, task := range p.tasks {
		for _, output := range task.outputs {
			outputs[output] = name
		}
	}

	allArchs := make(map[string]struct{}, len(archs))
	planned := make(map[string]struct{}, len(outputs)*len(allArchs))

	for _, arch := range archs {
		allArchs[arch] = struct{}{}
	}

	for _, target := range targets {
		p.targets[target] = make(map[string]struct{}, len(allArchs))

		for arch := range allArchs {
			p.targets[target][arch] = struct{}{}

			for _, output := range p.tasks[target].outputs {
				planned[path.Join(arch, output)] = struct{}{}
			}
		}
	}

	changed := true

	iter := 0
	for changed {
		if iter++; iter > 10 {
			panic("too many iterations")
		}

		changed = false

		for _, task := range targets {
			for _, input := range p.tasks[task].inputs {
				for arch := range allArchs {
					outname := path.Join(arch, input)

					if _, ok := planned[outname]; ok {
						continue
					} else if reuse {
						fmt.Printf("checking %s\n", outname)
						if stat, err := os.Stat(path.Join(p.outputDir, outname)); err == nil && stat.Mode().IsRegular() {
							fmt.Printf("skipping %s\n", outname)
							planned[outname] = struct{}{}

							continue
						} else if err != nil && !errors.Is(err, os.ErrNotExist) {
							panic(err)
						} else if err != nil {
							fmt.Printf("missing %s\n", outname)
						} else {
							fmt.Printf("not a file %s\n", outname)
						}
					}

					changed = true

					if name, ok := outputs[input]; !ok {
						panic("missing task for " + input)
					} else if tarTask := p.targets[name]; tarTask == nil {
						p.targets[name] = map[string]struct{}{arch: {}}
					} else {
						tarTask[arch] = struct{}{}
					}

					planned[outname] = struct{}{}
				}
			}
		}
	}
}

func (p *plan) wait(arch string, input string) error {
	ch := p.outputs[path.Join(arch, input)]

	if ch == nil {
		panic(fmt.Sprintf("missing output channel for %s/%s", arch, input))
	}

	<-ch

	return p.checkError()
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
		return err
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

	return err
}
