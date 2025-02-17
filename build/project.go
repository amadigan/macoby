package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/modfile"
)

func init() {
	//nolint:dogsled
	_, file, _, _ := runtime.Caller(0)

	for dir := filepath.Dir(file); dir != "/"; dir = filepath.Dir(dir) {
		stat, err := os.Stat(filepath.Join(dir, "go.mod"))
		if err == nil && stat.Mode().IsRegular() {
			buildPlan.root = dir

			// parse the go.mod to determine go version
			bs, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			if err != nil {
				panic(err)
			}

			modfile, err := modfile.Parse(filepath.Join(dir, "go.mod"), bs, nil)
			if err != nil {
				panic(err)
			}

			buildPlan.bakevars["go_version"] = modfile.Go.Version

			for _, r := range modfile.Require {
				if r.Mod.Path == "github.com/docker/cli" {
					version := r.Mod.Version
					version = strings.TrimPrefix(version, "v")
					version = strings.TrimSuffix(version, "+incompatible")
					buildPlan.bakevars["docker_version"] = "=~" + version
				}
			}
		}
	}
}
