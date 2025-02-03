package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/util"
)

func init() {
	buildPlan.tasks["rootfs"] = task{
		name:    "rootfs",
		outputs: []string{"share/railyard/linux/kernel", "share/railyard/linux/rootfs.img"},
		bake:    true,
	}
}

func (p *plan) bake(ctx context.Context) error {
	bakeTargets := map[string]struct{}{}

	for taskName, archs := range p.targets {
		if p.tasks[taskName].bake {
			for arch := range archs {
				bakeTargets[taskName+"-"+arch] = struct{}{}
			}
		}
	}

	if len(bakeTargets) == 0 {
		return nil
	}

	bakeArgs := append([]string{"buildx", "bake", "--file", "build/docker-bake.json"}, util.MapKeys(bakeTargets)...)

	log.Printf("docker %v", bakeArgs)

	cmd := exec.Command("docker", bakeArgs...)
	cmd.Dir = p.root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	env := make([]string, 0, len(p.bakevars)+1)
	for k, v := range p.bakevars {
		env = append(env, k+"="+v)
	}

	env = append(env, "output_dir="+p.outputDir)

	cmd.Env = append(os.Environ(), env...)

	start := time.Now()

	if err := cmd.Start(); err != nil {
		return err
	}

	p.mutex.Lock()
	p.processes["bake"] = cmd
	p.mutex.Unlock()

	err := cmd.Wait()
	done := time.Now()

	close(p.bakeChan)
	p.mutex.Lock()
	delete(p.processes, "bake")
	p.mutex.Unlock()

	duration := done.Sub(start)

	if err != nil {
		logf(ctx, applog.LogLevelError, "bake failed after %s: %v", duration, err)
	} else {
		logf(ctx, applog.LogLevelInfo, "bake done in %s", duration)
	}

	return err
}
