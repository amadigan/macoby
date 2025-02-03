package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/util"
)

func init() {
	applog.LogFormat = ">>>> %s %s %s\n"
}

func main() {
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
		panic(err)
	} else {
		buildPlan.outputDir = od
	}

	allArchitectures := []string{"amd64", "arm64"}

	switch archFlag {
	case "all":
	case "host":
		allArchitectures = []string{runtime.GOARCH}
	case "alien":
		for i, arch := range allArchitectures {
			if arch == runtime.GOARCH {
				allArchitectures = append(allArchitectures[:i], allArchitectures[i+1:]...)
				break
			}
		}
	default:
		found := false
		for _, arch := range allArchitectures {
			if arch == archFlag {
				found = true

				break
			}
		}

		if !found {
			panic(fmt.Errorf("unknown architecture %s", archFlag))
		}

		allArchitectures = []string{archFlag}
	}

	buildPlan.prepare(reuseFlag, flag.Args(), allArchitectures)

	fmt.Println("Targets to build:")

	for key, value := range buildPlan.targets {
		fmt.Printf("  %s: %v\n", key, util.MapKeys(value))
	}

	fmt.Printf("Output directory: %s\n", buildPlan.outputDir)
	fmt.Printf("Root directory: %s\n", buildPlan.root)
	fmt.Printf("Max Compression: %v\n", buildPlan.maxCompression)
	fmt.Printf("Dry Run: %v\n", buildPlan.dryrun)

	if buildPlan.dryrun {
		return
	}

	if err := os.Chdir(buildPlan.root); err != nil {
		panic(err)
	}

	defer func() {
		for _, proc := range buildPlan.processes {
			if proc.ProcessState == nil {
				fmt.Printf("sending interrupt to %s\n", proc)
				proc.Process.Signal(os.Interrupt)
			}
		}

		doneCh := make(chan struct{})

		go func() {
			for _, proc := range buildPlan.processes {
				if proc.ProcessState == nil {
					proc.Process.Wait()
				}
			}
		}()

		select {
		case <-doneCh:
		case <-time.After(3 * time.Second):
		}

		for _, proc := range buildPlan.processes {
			if proc.ProcessState == nil {
				fmt.Printf("sending kill to %s\n", proc)
				proc.Process.Kill()
			}
		}
	}()

	var wg sync.WaitGroup

	if err := startBuild(&wg); err != nil {
		panic(err)
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
		panic(err)
	}
}

func startBuild(wg *sync.WaitGroup) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

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

			ctx := context.WithValue(context.Background(), ctxkeyTask, buildTask)
			ctx = context.WithValue(ctx, ctykeyArch, arch)

			go func(ctx context.Context) {
				defer wg.Done()
				<-startCh

				btask := ctx.Value(ctxkeyTask).(task)  //nolint:forcetypeassert
				arch := ctx.Value(ctykeyArch).(string) //nolint:forcetypeassert

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
			}(ctx)
		}
	}

	close(startCh)

	if doBake {
		ctx := context.WithValue(context.Background(), ctxkeyTask, task{name: "bake"})
		return buildPlan.bake(ctx)
	}

	return nil
}
