package main

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/amadigan/macoby/internal/host"
	"github.com/amadigan/macoby/internal/host/config"
	"github.com/amadigan/macoby/internal/util"
)

func main() {
	env := util.Env()

	home, searchpath := config.BuildHomePath(env, "./local/")
	env[config.HomeEnv] = searchpath
	confPath := &config.Path{Original: fmt.Sprintf("${%s}/%s.jsonc", config.HomeEnv, config.Name)}

	if !confPath.ResolveInputFile(env, home) {
		panic(fmt.Errorf("failed to find %s.jsonc", config.Name))
	}

	var layout config.Layout
	if err := util.ReadJsonConfig(confPath.Resolved, &layout); err != nil {
		panic(fmt.Errorf("failed to read railyard.json: %w", err))
	}

	layout.SetDefaults()
	layout.SetDefaultSockets()

	if err := layout.ResolvePaths(env, home); err != nil {
		panic(fmt.Errorf("failed to resolve paths: %w", err))
	}

	cs := &host.ControlServer{
		Layout: &layout,
		Home:   home,
	}

	cs.SetupServer()

	network, address, err := parseSocket("./local/run/railyard.sock")
	if err != nil {
		panic(err)
	}

	listener, err := net.Listen(network, address)
	if err != nil {
		panic(err)
	}

	fmt.Printf("listening on %s\n", listener.Addr())
	fmt.Println(cs.Serve(listener))
}

func parseSocket(s string) (string, string, error) {
	parts := strings.SplitN(s, ":", 3)

	switch len(parts) {
	case 1:
		abs, err := filepath.Abs(parts[0])
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve path %s: %w", parts[0], err)
		}

		return "unix", abs, nil
	case 2:
		switch parts[0] {
		case "unix":
			abs, err := filepath.Abs(parts[1])
			if err != nil {
				return "", "", fmt.Errorf("failed to resolve path %s: %w", parts[1], err)
			}

			return "unix", abs, nil
		case "tcp":
			fallthrough
		case "tcp4":
			fallthrough
		case "tcp6":
			return parts[0], ":" + parts[1], nil
		default:
			return "", "", fmt.Errorf("invalid socket type %s", parts[0])
		}
	case 3:
		switch parts[0] {
		case "unix":
			abs, err := filepath.Abs(parts[1] + ":" + parts[2])
			if err != nil {
				return "", "", fmt.Errorf("failed to resolve path %s: %w", parts[1], err)
			}

			return "unix", abs, nil
		case "tcp":
			fallthrough
		case "tcp4":
			fallthrough
		case "tcp6":
			return parts[0], parts[1] + ":" + parts[2], nil
		default:
			return "", "", fmt.Errorf("invalid socket type %s", parts[0])
		}
	}

	return "", "", fmt.Errorf("invalid socket %s", s)
}
