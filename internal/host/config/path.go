package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Path struct {
	Original string
	Resolved string
}

type pathVariable struct {
	values  []string
	current int
}

func (p *Path) MarshalText() ([]byte, error) {
	if p.Resolved != "" {
		return []byte(p.Resolved), nil
	}

	return []byte(p.Original), nil
}

func (p *Path) UnmarshalText(data []byte) error {
	p.Original = string(data)

	return nil
}

type Paths []*Path

func (p Paths) MarshalJSON() ([]byte, error) {
	if len(p) == 1 {
		return json.Marshal(p[0])
	}

	return json.Marshal([]*Path(p))
}

func (p *Paths) UnmarshalJSON(data []byte) error {
	var str string

	if err := json.Unmarshal(data, &str); err == nil {
		if str == "" {
			return nil
		}

		*p = Paths{{Original: str}}

		return nil
	}

	paths := []*Path{}

	if err := json.Unmarshal(data, &paths); err != nil {
		//nolint:wrapcheck
		return err
	}

	for _, path := range paths {
		if path.Original != "" {
			*p = append(*p, path)
		}
	}

	return nil
}

func (p *Path) ResolveInputFile(env map[string]string, root string) bool {
	for _, value := range interpolateOptions(p.Original, env) {
		if !filepath.IsAbs(value) {
			value = filepath.Join(root, value)
		}

		if stat, err := os.Stat(value); err == nil && stat.Mode().IsRegular() {
			p.Resolved = value

			return true
		}
	}

	return false
}

func (p *Path) ResolveInputDir(env map[string]string, root string) bool {
	for _, value := range interpolateOptions(p.Original, env) {
		if !filepath.IsAbs(value) {
			value = filepath.Join(root, value)
		}

		if stat, err := os.Stat(value); err == nil && stat.Mode().IsDir() {
			p.Resolved = value

			return true
		}
	}

	return false
}

func (p *Path) ResolveOutputFile(env map[string]string, root string) (bool, error) {
	value := interpolate(p.Original, env)

	if !filepath.IsAbs(value) {
		value = filepath.Join(root, value)
	}

	if stat, err := os.Stat(value); err == nil && stat.Mode().IsRegular() {
		p.Resolved = value

		return true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("failed to stat %s: %w", value, err)
	}

	dir := filepath.Dir(value)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	p.Resolved = value

	return false, nil
}

var networks = map[string]bool{
	"tcp":  true,
	"tcp4": true,
	"tcp6": true,
}

func resolveListenSocket(path, root string) (network string, addr string, err error) {
	if parts := strings.SplitN(path, ":", 2); len(parts) == 2 && networks[parts[0]] {
		return parts[0], parts[1], nil
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}

	if stat, err := os.Stat(path); err == nil && stat.Mode().Type()&os.ModeSocket != 0 {
		return "unix", path, nil
	}

	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	return "unix", path, nil
}

func (p Path) ResolveListenSocket(env map[string]string, root string) (network string, addr string, err error) {
	return resolveListenSocket(interpolate(p.Original, env), root)
}

func (p *Path) ResolveOutputDir(env map[string]string, root string) error {
	value := interpolate(p.Original, env)

	if !filepath.IsAbs(value) {
		value = filepath.Join(root, value)
	}

	if stat, err := os.Stat(value); os.IsNotExist(err) {
		if err := os.MkdirAll(value, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", value, err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to stat %s: %w", value, err)
	} else if !stat.Mode().IsDir() {
		return fmt.Errorf("not a directory: %s", value)
	}

	p.Resolved = value

	return nil
}

func (l *Layout) ResolvePaths(env map[string]string, root string) error {
	if !l.Kernel.ResolveInputFile(env, root) {
		return fmt.Errorf("kernel file not found: %s", l.Kernel.Original)
	}

	if !l.Root.ResolveInputFile(env, root) {
		return fmt.Errorf("root file not found: %s", l.Root.Original)
	}

	if _, err := l.StateFile.ResolveOutputFile(env, root); err != nil {
		return err
	}

	for label, disk := range l.Disks {
		if _, err := disk.Path.ResolveOutputFile(env, root); err != nil {
			return fmt.Errorf("cannot resolve disk path %s: %w", label, err)
		}
	}

	for dst, share := range l.Shares {
		if !share.Source.ResolveInputDir(env, root) {
			delete(l.Shares, dst)
		}
	}

	return nil
}
