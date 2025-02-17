package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/amadigan/macoby/internal/applog"
)

func init() {
	buildPlan.tasks["zip"] = task{
		name:   "zip",
		inputs: []string{"bin/railyard", "share/railyard/linux/kernel", "share/railyard/linux/rootfs.img"},
		run:    buildPlan.buildZip,
	}
}

func (p *plan) buildZip(ctx context.Context, t task, arch string) error {
	railyardBin := "bin/railyard"

	if err := p.wait(arch, railyardBin); err != nil {
		return err
	}

	ts := time.Now()

	f, err := os.Create(filepath.Join(p.outputDir, fmt.Sprintf("railyard-%s.zip", arch)))
	if err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}

	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()

	if err := addFileToZip(ctx, zw, filepath.Join(p.outputDir, arch, railyardBin), "railyard/"+railyardBin, 0755, p.maxCompression, ts); err != nil {
		return fmt.Errorf("failed to add %s to zip: %w", railyardBin, err)
	}

	kernel := "share/railyard/linux/kernel"

	if err := p.wait(arch, kernel); err != nil {
		return err
	}

	compression := p.maxCompression

	if arch == "amd64" {
		compression = zip.Store
	}

	if err := addFileToZip(ctx, zw, filepath.Join(p.outputDir, arch, kernel), "railyard/"+kernel, 0644, compression, ts); err != nil {
		return fmt.Errorf("failed to add %s to zip: %w", kernel, err)
	}

	rootfs := "share/railyard/linux/rootfs.img"

	if err := p.wait(arch, rootfs); err != nil {
		return err
	}

	if err := addFileToZip(ctx, zw, filepath.Join(p.outputDir, arch, rootfs), "railyard/"+rootfs, 0644, zip.Store, ts); err != nil {
		return fmt.Errorf("failed to add %s to zip: %w", rootfs, err)
	}

	if err := addFileToZip(ctx, zw, filepath.Join(p.root, "LICENSE.txt"), "railyard/LICENSE.txt", 0644, zip.Deflate, ts); err != nil {
		return fmt.Errorf("failed to add LICENSE.txt to zip: %w", err)
	}

	return nil
}

func addFileToZip(ctx context.Context, zw *zip.Writer, path string, name string, mode os.FileMode, method uint16, ts time.Time) error {
	var rawSize int64

	if info, err := os.Stat(path); err != nil {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	} else {
		rawSize = info.Size()
	}

	fh := &zip.FileHeader{
		Name:               name,
		Method:             method,
		UncompressedSize64: uint64(rawSize), //nolint:gosec
		CreatorVersion:     3<<8 | 63,       // Unix with XZ/Zstd compression
		ExternalAttrs:      uint32(mode&0xfff) << 16,
		Modified:           ts,
	}

	w, err := zw.CreateHeader(fh)
	if err != nil {
		return fmt.Errorf("failed to create header for %s: %w", name, err)
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", path, err)
	}

	defer f.Close()

	start := time.Now()

	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("failed to copy %s to zip: %w", path, err)
	}

	duration := time.Since(start)

	logf(ctx, applog.LogLevelInfo, "added %s with method %d in %s", path, method, duration)

	return nil
}
