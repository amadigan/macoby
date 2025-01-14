package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/amadigan/macoby/internal/util"
)

const Name = "railyard"
const AppID = "com.github.amadigan.railyard"
const Version = "0.1.0"
const HomeEnv = "RAILYARD_HOME"
const SysHomeDir = "/usr/local/share/railyard"
const UserHomeDir = "${HOME}/Library/Application Support/railyard"

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

	paths.PushBack(interpolate(UserHomeDir, env))
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

func LoadConfig(env map[string]string, home string) (*Layout, string, error) {
	home, searchpath := BuildHomePath(env, home)
	env[HomeEnv] = searchpath

	confPath := &Path{Original: "${RAILYARD_HOME}/railyard.jsonc"}

	if !confPath.ResolveInputFile(env, home) {
		return nil, home, fmt.Errorf("failed to find %s.jsonc", Name)
	}

	var layout Layout
	if err := util.ReadJsonConfig(confPath.Resolved, &layout); err != nil {
		return nil, home, fmt.Errorf("failed to read railyard.json: %w", err)
	}

	layout.SetDefaults()

	if err := layout.ResolvePaths(env, home); err != nil {
		return &layout, home, fmt.Errorf("failed to resolve paths: %w", err)
	}

	return &layout, home, nil
}
