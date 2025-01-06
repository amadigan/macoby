package host

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DataDir = "data"

var sysDirs = []string{
	"/usr/local/share/" + Name,
	"/Library/Application Support/" + AppID,
}

type FileResolver struct {
	Home     string
	scanDirs []string
}

func NewFileResolver() (*FileResolver, error) {
	envName := strings.ToUpper(Name) + "_HOME"
	home := os.Getenv(envName)

	rv := &FileResolver{scanDirs: []string{""}}

	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), "."+Name)
	} else {
		var err error

		if home, err = filepath.Abs(home); err != nil {
			return nil, fmt.Errorf("failed to resolve %s: %w", envName, err)
		}
	}

	libhome := filepath.Join(os.Getenv("HOME"), "Library", "Application Support", AppID)

	if stat, err := os.Stat(home); err != nil || !stat.IsDir() {
		rv.Home = libhome
	} else if stat, err := os.Stat(libhome); err == nil && stat.IsDir() {
		rv.Home = home
		rv.scanDirs = append(rv.scanDirs, libhome)
	}

	rv.scanDirs[0] = rv.Home

	for _, dir := range sysDirs {
		if stat, err := os.Stat(dir); err == nil && stat.IsDir() {
			rv.scanDirs = append(rv.scanDirs, dir)
		}
	}

	return rv, nil
}

func (r *FileResolver) PrependScanDir(dir string) {
	r.scanDirs = append([]string{dir}, r.scanDirs...)
}

func (r *FileResolver) SetHome(home string) {
	r.PrependScanDir(home)
	r.Home = home
}

func (r *FileResolver) ResolveInputFile(names ...string) string {
	for _, name := range names {
		if !filepath.IsAbs(name) {
			for _, dir := range r.scanDirs {
				path := filepath.Join(dir, name)

				if stat, err := os.Stat(path); err == nil && stat.Mode().IsRegular() {
					return path
				}
			}
		} else if stat, err := os.Stat(name); err == nil && stat.Mode().IsRegular() {
			return name
		}
	}

	return ""
}

func (r *FileResolver) ResolveInputDir(name string) string {
	if !filepath.IsAbs(name) {
		for _, dir := range r.scanDirs {
			path := filepath.Join(dir, name)

			if stat, err := os.Stat(path); err == nil && stat.Mode().IsDir() {
				return path
			}
		}
	} else if stat, err := os.Stat(name); err == nil && stat.Mode().IsDir() {
		return name
	}

	return ""
}

func (r *FileResolver) ResolveOutputFile(name string) (string, error) {
	if !filepath.IsAbs(name) {
		name = filepath.Join(r.Home, name)
	}

	dir := filepath.Dir(name)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	return name, nil
}

func (r *FileResolver) ResolveOutputDir(name string) (string, error) {
	if !filepath.IsAbs(name) {
		name = filepath.Join(r.Home, name)
	}

	if err := os.MkdirAll(name, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", name, err)
	}

	return name, nil
}

func (r *FileResolver) ResolveSocket(name string) (string, error) {
	if !filepath.IsAbs(name) {
		name = filepath.Join(r.Home, name)
	}

	dir := filepath.Dir(name)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	return name, nil
}

func (r *FileResolver) ResolvePaths(layout *Layout) error {
	kernel := r.ResolveInputFile(layout.Kernel)

	if kernel == "" {
		return fmt.Errorf("kernel %s not found", layout.Kernel)
	}

	layout.Kernel = kernel

	root := r.ResolveInputFile(layout.Root)

	if root == "" {
		return fmt.Errorf("root %s not found", layout.Root)
	}

	layout.Root = root

	for label, disk := range layout.Disks {
		if disk.Path == "" {
			disk.Path = filepath.Join(DataDir, label+".img")
		}

		if path, err := r.ResolveOutputFile(disk.Path); err != nil {
			return fmt.Errorf("disk %s path %s: %w", label, disk.Path, err)
		} else if path != "" {
			disk.Path = path
		} else {
			return fmt.Errorf("disk %s path %s not found", label, disk.Path)
		}
	}

	for _, share := range layout.Shares {
		if src := r.ResolveInputDir(share.Source); src == "" {
			return fmt.Errorf("share %s not found", share.Source)
		} else {
			share.Source = src
		}
	}

	if layout.DockerSocket.HostPath != "" {
		if path, err := r.ResolveOutputFile(layout.DockerSocket.HostPath); err != nil {
			return fmt.Errorf("docker socket %s: %w", layout.DockerSocket.HostPath, err)
		} else if path != "" {
			layout.DockerSocket.HostPath = path
		} else {
			return fmt.Errorf("cannot create docker socket %s", layout.DockerSocket.HostPath)
		}
	}

	if layout.ControlSocket != "" {
		if path, err := r.ResolveSocket(layout.ControlSocket); err != nil {
			return fmt.Errorf("control socket %s: %w", layout.ControlSocket, err)
		} else if path != "" {
			layout.ControlSocket = path
		} else {
			return fmt.Errorf("cannot create control socket %s", layout.ControlSocket)
		}
	}

	if layout.StateFile != "" {
		if path, err := r.ResolveOutputFile(layout.StateFile); err != nil {
			return fmt.Errorf("state file %s: %w", layout.StateFile, err)
		} else if path != "" {
			layout.StateFile = path
		} else {
			return fmt.Errorf("cannot create state file %s", layout.StateFile)
		}
	}

	return nil
}
