package image

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/amadigan/macoby/internal/arch"
	"github.com/amadigan/macoby/internal/util"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/google/uuid"
)

var InstallerPartitionId = uuid.MustParse("ab399a56-0218-4d84-82a8-c0a5c57e772c")
var RootType = uuid.MustParse("d639ffbe-d8ce-4ad6-8e4a-7ba03ed42498")

const blockSize = 512
const distAlignment = 512 * 1024

type SystemImageSpec struct {
	DiskID       uuid.UUID
	BootID       uuid.UUID
	RootID       uuid.UUID
	RootType     uuid.UUID
	Root         string
	RootConfig   RootConfig
	Compression  util.CompressMode
	Architecture arch.Architecture
}

type RootConfig struct {
	Mountpoints []string
	Symlinks    map[string]string
}

var DefaultConfig RootConfig = RootConfig{
	Mountpoints: []string{"/boot", "/sys", "/proc", "/etc", "/cgroup1", "/dev", "/run", "/tmp", "/var/lib/docker"},
	Symlinks: map[string]string{
		"/var/run": "../run",
	},
}

func (spec *SystemImageSpec) setDefaults() {
	if spec.DiskID == uuid.Nil {
		spec.DiskID = uuid.New()
	}

	if spec.BootID == uuid.Nil {
		spec.BootID = uuid.New()
	}

	if spec.RootID == uuid.Nil {
		spec.RootID = uuid.New()
	}

	if spec.Compression == "" {
		spec.Compression = util.CompressNone
	}
}

func (spec SystemImageSpec) Create(out string) (*gpt.Table, error) {
	spec.setDefaults()
	writer := &imageWriter{
		table: &gpt.Table{
			LogicalSectorSize:  blockSize,
			PhysicalSectorSize: blockSize,
			GUID:               spec.DiskID.String(),
		},
		path: out,
	}

	if spec.Root != "" {
		if root, err := writer.openExternal(gpt.Type(RootType.String()), "root", spec.RootID.String()); err == nil {
			if err := spec.squash(root, out); err != nil {
				return nil, err
			}

			if err := root.close(); err != nil {
				return nil, fmt.Errorf("error closing root partition: %v", err)
			}
		} else {
			return nil, err
		}
	}

	return writer.table, writer.finish()
}

func exists(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, err
	}
}

func (spec SystemImageSpec) writeSquashConfigs() (pfPath string, ePath string, fErr error) {
	pfPath = path.Join(spec.Root, "squashfs-pf")
	var pfWriter *os.File
	pfWriter, fErr = os.OpenFile(pfPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)

	if fErr != nil {
		return
	}

	defer func() {
		len, _ := pfWriter.Seek(0, 1)
		pfWriter.Close()
		if fErr != nil || len <= 0 {
			os.Remove(pfPath)
			pfPath = ""
		}
	}()

	ePath = path.Join(spec.Root, "squashfs-exclude")
	var eWriter *os.File
	eWriter, fErr = os.OpenFile(ePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)

	if fErr != nil {
		return
	}

	defer func() {
		len, _ := eWriter.Seek(0, 1)
		eWriter.Close()
		if fErr != nil || len <= 0 {
			os.Remove(ePath)
			ePath = ""
		}
	}()

	directories := make(map[string]bool)
	directories["/"] = true

	for _, mpoint := range spec.RootConfig.Mountpoints {
		mpoint = path.Clean(mpoint)
		mpath := path.Join(spec.Root, "root", mpoint)

		if ex, err := exists(mpath); ex {
			eWriter.WriteString(mpath + "\n")
		} else if err != nil {
			fErr = err
			return
		}

		for ; !directories[mpoint]; mpoint = path.Dir(mpoint) {
			directories[mpoint] = true
			if _, err := pfWriter.WriteString(mpoint + " d 555 0 0\n"); err != nil {
				fErr = err
				return
			}
		}
	}

	for dst, src := range spec.RootConfig.Symlinks {
		dst = path.Clean(dst)

		if _, err := pfWriter.WriteString(fmt.Sprintf("%s s 555 0 0 %s\n", dst, src)); err != nil {
			fErr = err
			return
		}

		for parent := path.Dir(dst); !directories[parent]; parent = path.Dir(parent) {
			directories[parent] = true
			if _, err := pfWriter.WriteString(parent + " d 555 0 0\n"); err != nil {
				fErr = err
				return
			}
		}
	}

	return
}

func (spec SystemImageSpec) squash(root *externalPartition, out string) error {
	squashArgs := []string{path.Join(spec.Root, "root"), out, "-no-exports", "-noappend", "-offset", strconv.FormatInt(root.getOffset(), 10)}

	if pfPath, ePath, err := spec.writeSquashConfigs(); err == nil {
		if pfPath != "" {
			defer os.Remove(pfPath)
			squashArgs = append(squashArgs, "-pf", pfPath)
		}

		if ePath != "" {
			defer os.Remove(ePath)
			squashArgs = append(squashArgs, "-ef", ePath)
		}
	} else {
		return err
	}

	switch spec.Compression {
	case util.CompressNone:
		squashArgs = append(squashArgs, "-noInodeCompression", "-noDataCompression", "-noFragmentCompression", "-noXattrCompression")
	case util.CompressGZip:
		squashArgs = append(squashArgs, "-comp", "gzip")
	case util.CompressLZMA:
		squashArgs = append(squashArgs, "-comp", "lzma")
	case util.CompressLZO:
		squashArgs = append(squashArgs, "-comp", "lzo", "-Xcompression-level", "9")
	case util.CompressLZ4:
		squashArgs = append(squashArgs, "-comp", "lz4", "-Xhc")
	case util.CompressXZ:
		squashArgs = append(squashArgs, "-comp", "xz")
		switch spec.Architecture.FullCPU {
		case arch.X86_64, arch.I386:
			squashArgs = append(squashArgs, "-Xbcj", "x86")
		case arch.ARM64:
			squashArgs = append(squashArgs, "-Xbcj", "arm")
		}
	}

	log.Printf("mksquashfs %s", strings.Join(squashArgs, " "))

	cmd := exec.Command("mksquashfs", squashArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to generate SquashFS image: %v", err)
	}

	return nil
}
