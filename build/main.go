package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/util"
)

func init() {
	applog.LogFormat = ">>>> %s %s %s\n"
}

func parseFlags(ctx context.Context) error {
	var archFlag string
	var reuseFlag bool
	var outputDir string
	var localpkg string

	flag.StringVar(&archFlag, "arch", runtime.GOARCH, "target architecture")
	flag.BoolVar(&reuseFlag, "reuse", false, "reuse existing build artifacts")
	flag.StringVar(&outputDir, "output", filepath.Join(buildPlan.root, "dist"), "output directory")
	flag.StringVar(&localpkg, "localpkg", "localpkg", "localpkg command or URL")
	flag.StringVar(&buildPlan.codesigner, "codesigner", "", "codesign identity")
	flag.BoolVar(&buildPlan.dryrun, "dryrun", false, "dry run")

	flag.Parse()

	if od, err := filepath.Abs(outputDir); err != nil {
		logf(ctx, applog.LogLevelError, "failed to get absolute path for %s: %v", outputDir, err)
	} else {
		buildPlan.outputDir = od
	}

	allArchitectures := parseArchFlag(archFlag)

	return buildPlan.prepare(ctx, reuseFlag, flag.Args(), allArchitectures)
}

func main() {
	ctx := context.WithValue(context.Background(), ctxkeyTask, task{name: "main"})
	if err := parseFlags(ctx); err != nil {
		logf(ctx, applog.LogLevelError, "failed to parse flags: %v", err)

		os.Exit(1)
	}

	entries := make([]string, 0, len(buildPlan.targets))

	for key, value := range buildPlan.targets {
		entries = append(entries, fmt.Sprintf("%s (%s)", key, strings.Join(util.MapKeys(value), ", ")))
	}

	logf(ctx, applog.LogLevelInfo, "Targets to build: %s", strings.Join(entries, ", "))
	logf(ctx, applog.LogLevelInfo, "Output directory: %s", buildPlan.outputDir)
	logf(ctx, applog.LogLevelInfo, "Build root: %s", buildPlan.root)
	logf(ctx, applog.LogLevelInfo, "Max compression: %v", buildPlan.maxCompression)
	logf(ctx, applog.LogLevelInfo, "Dry run: %v", buildPlan.dryrun)

	if buildPlan.dryrun {
		return
	}

	if err := os.Chdir(buildPlan.root); err != nil {
		logf(ctx, applog.LogLevelError, "failed to change directory: %v", err)
	}

	defer buildPlan.shutdown(ctx)

	wg, err := startBuild(ctx)
	if err != nil {
		logf(ctx, applog.LogLevelError, "failed to start build: %v", err)
	}

	logDone := make(chan struct{})

	go func() {
		defer close(logDone)
		logLoop()
	}()

	wg.Wait()
	close(buildPlan.logchan)
	<-logDone

	if err := buildPlan.checkError(); err != nil {
		logf(ctx, applog.LogLevelError, "build failed: %v", err)
	}
}

func parseArchFlag(archFlag string) []string {
	allArchitectures := []string{"amd64", "arm64"}

	switch archFlag {
	case "all":
	case "host":
		allArchitectures = []string{runtime.GOARCH}
	case "alien":
		if ind := slices.Index(allArchitectures, runtime.GOARCH); ind != -1 {
			allArchitectures = slices.Delete(allArchitectures, ind, ind+1)
		}
	default:
		if !slices.Contains(allArchitectures, archFlag) {
			panic(fmt.Errorf("unknown architecture %s", archFlag))
		}

		allArchitectures = []string{archFlag}
	}

	return allArchitectures
}

func startBuild(ctx context.Context) (wg *sync.WaitGroup, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	wg = &sync.WaitGroup{}

	startCh := make(chan struct{})
	doBake := false

	for target, archs := range buildPlan.targets {
		buildTask := buildPlan.tasks[target]

		if buildTask.bake {
			doBake = true
		}

		for arch := range archs {
			wg.Add(1)

			for _, output := range buildTask.outputs {
				outname := path.Join(arch, output)
				buildPlan.outputs[outname] = make(chan struct{})
			}

			ctx := context.WithValue(ctx, ctxkeyTask, buildTask)
			ctx = context.WithValue(ctx, ctykeyArch, arch)
			ctx = redirectLogs(ctx, buildPlan.logchan)

			go func(ctx context.Context) {
				defer wg.Done()
				<-startCh

				runBuildTask(ctx)
			}(ctx)
		}
	}

	close(startCh)

	if doBake {
		ctx := context.WithValue(ctx, ctxkeyTask, task{name: "bake"})

		return wg, buildPlan.bake(ctx)
	}

	return wg, nil
}

func runBuildTask(ctx context.Context) {
	btask, _ := ctx.Value(ctxkeyTask).(task)
	arch, _ := ctx.Value(ctykeyArch).(string)

	if btask.bake {
		<-buildPlan.bakeChan
	}

	if btask.run != nil {
		start := time.Now()

		logf(ctx, applog.LogLevelInfo, "starting task %s-%s", btask.name, arch)

		if err := btask.run(ctx, btask, arch); err != nil {
			logf(ctx, applog.LogLevelError, "failed: %v", err)
			buildPlan.setError(err)
		}

		logf(ctx, applog.LogLevelInfo, "finished task %s-%s in %s", btask.name, arch, time.Since(start))
	}

	for _, output := range btask.outputs {
		outname := path.Join(arch, output)
		close(buildPlan.outputs[outname])
	}
}
