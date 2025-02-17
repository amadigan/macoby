package main

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/amadigan/macoby/internal/sysctl"
	"github.com/amadigan/macoby/internal/util"
)

// this tool executes during the creation of the root filesystem image. It merges the contents of the various
// sysctl.d directories into /etc/sysctl.conf and deletes the directories.
// /etc/sysctl.conf is loaded by init on boot and the sysctl values from the Init command are applied.

var sysctlDirs = []string{"/usr/lib/sysctl.d", "/lib/sysctl.d", "/etc/sysctl.d"}
var sysctlFile = "/etc/sysctl.conf"

func listSysctlFiles(root fs.ReadDirFS) ([]string, error) {
	files := map[string]string{}
	for _, dir := range sysctlDirs {
		entries, err := root.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".conf") {
				files[entry.Name()] = filepath.Join(dir, entry.Name())
			}
		}
	}

	rv := util.MapValues(files)

	if stat, err := os.Stat(sysctlFile); err == nil && stat.Mode().IsRegular() {
		rv = append(rv, sysctlFile)
	}

	return rv, nil
}

func compileSysctls(root string) error {
	rtfs, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer rtfs.Close()

	rdfs, _ := rtfs.FS().(fs.ReadDirFS)
	files, err := listSysctlFiles(rdfs)
	if err != nil {
		return err
	}

	ctls, err := sysctl.LoadSysctls(rdfs, files...)
	if err != nil {
		return err
	}

	f, err := os.Create(filepath.Join(root, sysctlFile))
	if err != nil {
		return err
	}
	defer f.Close()

	bw := bufio.NewWriter(f)

	for key, value := range ctls {
		_, _ = bw.WriteString(key + "=" + value + "\n")
	}

	if err := bw.Flush(); err != nil {
		return err
	}

	for _, dir := range sysctlDirs {
		_ = os.RemoveAll(filepath.Join(root, dir))
	}

	return nil
}
