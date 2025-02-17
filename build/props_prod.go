//go:build prod

package main

import "github.com/amadigan/macoby/build/compress"

func init() {
	buildPlan.bakevars["gobuild_flags"] = "-trimpath"
	buildPlan.bakevars["gobuild_ldflags"] = "-s -w"
	buildPlan.bakevars["zstd_level"] = "22"

	buildPlan.gobuildargs = append(buildPlan.gobuildargs, "-trimpath", "-ldflags=-s -w")

	buildPlan.maxCompression = compress.XZ_METHOD
}
