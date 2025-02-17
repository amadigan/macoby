package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/util"
	"gopkg.in/yaml.v2"
)

const Name = "railyard"
const AppID = "com.github.amadigan.railyard"
const Version = "0.1.0"
const HomeEnv = "RAILYARD_HOME"
const SysHomeDir = "/usr/local/share/railyard"
const UserHomeDir = "${HOME}/Library/Application Support/railyard"

var log = applog.New("config")

var packages = map[string]string{
	"LOCALPKG_PREFIX": "${HOME}/.local",
	"HOMEBREW_PREFIX": "/opt/homebrew",
}

func BuildHomePath(env map[string]string, path string) (string, string) {
	paths := util.List[string]{}

	home := path

	if home == "" {
		home = env[HomeEnv]
	}

	for _, part := range strings.Split(home, ":") {
		if part != "" {
			if abs, err := filepath.Abs(part); err == nil {
				paths.PushBack(abs)
			}
		}
	}

	// TODO this should be *after* the user's home directory
	if ep, err := os.Executable(); err != nil {
		log.Warnf("failed to get executable path: %v", err)
	} else if ep, err = filepath.EvalSymlinks(ep); err != nil {
		// TODO handle symlink permutations
		log.Warnf("failed to resolve executable symlink: %v", err)
	} else {
		dir := filepath.Join(filepath.Dir(ep), "..", "share", Name)
		if abs, err := filepath.Abs(dir); err == nil {
			paths.PushBack(abs)
		}
	}

	paths.PushBack(interpolate(UserHomeDir, env))

	for key, value := range packages {
		if val := env[key]; val != "" {
			value = val
		} else {
			value = interpolate(value, env)
		}

		paths.PushBack(filepath.Join(value, "share", Name))
	}

	paths.PushBack(interpolate(SysHomeDir, env))

	set := map[string]struct{}{}
	parts := make([]string, 0, paths.Len())

	for path := range paths.FromFront() {
		if _, ok := set[path]; ok {
			continue
		}

		set[path] = struct{}{}

		parts = append(parts, path)
	}

	front, _ := paths.Front()

	return front, strings.Join(parts, ":")
}

//go:embed railyard.yaml
var defaultConfig []byte

func LoadConfig(env map[string]string, home string) (*Layout, string, error) {
	home, searchpath := BuildHomePath(env, home)
	env[HomeEnv] = searchpath

	confPath := &Path{Original: fmt.Sprintf("${%s}/%s.yaml", HomeEnv, Name)}

	var configBytes []byte

	if !confPath.ResolveInputFile(env, home) {
		log.Infof("failed to resolved %s.yaml, using default config", Name)

		configBytes = defaultConfig
	} else if bs, err := os.ReadFile(confPath.Resolved); err != nil {
		return nil, home, fmt.Errorf("failed to read %s: %w", confPath.Resolved, err)
	} else {
		configBytes = bs
	}

	var layout Layout

	if err := yaml.Unmarshal(configBytes, &layout); err != nil {
		return nil, home, fmt.Errorf("failed to read parse %s: %w", confPath.Resolved, err)
	}

	layout.SetDefaults()

	if err := layout.ResolvePaths(env, home); err != nil {
		return &layout, home, fmt.Errorf("failed to resolve paths: %w", err)
	}

	return &layout, home, nil
}
