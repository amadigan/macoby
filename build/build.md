# Build Process

The railyard build has two major components:

- the Docker build, which builds the kernel and root filesystem image for each target architecture
- the macOS build, which builds the railyard executable and packages

For local development, both components can easily be run on a macOS host with:

- a working docker installation (any of: railyard itself, Docker Deskop, a remote Docker host, etc.)
- Xcode Command Line Tools
- the current version of Go

On GitHub Actions, the Docker build runs on an `ubuntu` runner and the `macos` build runs on a macOS runner. For the
Docker build, caching is handled through the docker cache mechanism. For the macOS build, Go module caching is used.

Note that a full build from source can easily take 15 minutes or more, depending on the host system. Most of this is
the build time for the kernel, which is part of the Docker build.

## Running the build

The build itself is written in Go, to execute it, run `go run ./build`. The build accepts a series of options followed
by a list of build targets.

Options:

--arch `target`		architecture, valid values (host, alien, arm64, amd64, all) (default: host)
--reuse 					reuse existing build artifacts for targets not explicitly specified
--output `dir`  	write build artifacts to `dir` (default: current working directory, or root/dist if cwd is root)
--localpkg `PATH` use command or URL for localpkg, pass empty string to disable (default: local path or latest release)
--props `PATH` 		read build properties YAML from `PATH` (default: root/build/props.yaml)

Targets:

- `kernel` - build the kernel for the specified architecture
- `rootfs` - build the root filesystem for the specified architecture
- `railyard` - build the railyard executable for the specified architecture
- `zip` - build the ZIP and localpkg script
- `pkg` - build the .pkg installer package
- `all` - build all targets (alias for `zip pkg`)
- `clean` - clean the build directory

The build creates artifacts in the output directory, which must not be the project root. A full build will create the
following files:

/arm64/bin/railyard
/arm64/share/railyard/linux/kernel
/arm64/share/railyard/linux/rootfs.img
/railyard-arm64.zip
/railyard-arm64.pkg
/amd64/bin/railyard
/amd64/share/railyard/linux/kernel
/amd64/share/railyard/linux/rootfs.img
/railyard-x86_64.zip
/railyard-x86_64.pkg
/railyard.localpkg

Specifying a target on the command line builds that target, and all dependencies. In general, the built-in change
detection mechanisms in Docker and go build will prevent unnecessary work. To skip targets that already exist and are
not specified on the command line, use the `--reuse` flag. Note that for this purpose, "all" is exactly the same
as specifying `zip pkg`.

## Parallelism

The build is parallelized for maximum speed. 
