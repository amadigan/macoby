package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Code-Hex/vz/v3"
	"github.com/amadigan/macoby/internal/event"
	"github.com/amadigan/macoby/internal/host/config"
	"github.com/amadigan/macoby/internal/host/disk"
	"github.com/amadigan/macoby/internal/rpc"
	"github.com/amadigan/macoby/internal/util"
	"golang.org/x/sys/unix"
)

type diskMount struct {
	mountpoint string
	mountFunc  func(*VirtualMachine) error
}

func newBlockDevice(path string, readOnly bool, cache vz.DiskImageCachingMode, sync vz.DiskImageSynchronizationMode) (vz.StorageDeviceConfiguration, error) {
	attachment, err := vz.NewDiskImageStorageDeviceAttachmentWithCacheAndSync(path, readOnly, cache, sync)
	if err != nil {
		return nil, fmt.Errorf("failed to create disk %s attachment: %w", path, err)
	}

	cfg, err := vz.NewVirtioBlockDeviceConfiguration(attachment)
	if err != nil {
		return nil, fmt.Errorf("failed to create disk %s configuration: %w", path, err)
	}

	return cfg, nil
}

func (vm *VirtualMachine) prepareDisks() error {
	log.Debugf("root disk: %s", vm.Layout.Root)

	rootImage, err := newBlockDevice(vm.Layout.Root.Resolved, true, vz.DiskImageCachingModeCached, vz.DiskImageSynchronizationModeNone)
	if err != nil {
		return fmt.Errorf("failed to create root disk %s configuration: %w", vm.Layout.Root, err)
	}

	vm.storages = append(vm.storages, rootImage)

	var emptyDisks []*config.DiskImage

	for _, label := range util.SortKeys(vm.Layout.Disks) {
		diskInfo := vm.Layout.Disks[label]
		if diskInfo.Mount == "" {
			continue
		}

		log.Debugf("disk %s: %s -> %s", label, diskInfo.Path, diskInfo.Mount)

		var size int64
		var err error

		if diskInfo.Size != "" {
			if size, err = config.ParseSize(diskInfo.Size); err != nil {
				return fmt.Errorf("failed to parse disk %s size: %s: %w", label, diskInfo.Size, err)
			}
		}

		var fsIdentify func() (*disk.Filesystem, error)

		stat, err := os.Stat(diskInfo.Path.Resolved)
		if size == 0 && err == nil {
			size = stat.Size()
			fsIdentify = func() (*disk.Filesystem, error) {
				return nil, nil
			}

			emptyDisks = append(emptyDisks, diskInfo)
		} else if errors.Is(err, os.ErrNotExist) || (err == nil && stat.Size() < size) {
			if err := setFileSize(diskInfo.Path.Resolved, size); err != nil {
				return fmt.Errorf("failed to create disk %s (%s): %w", label, diskInfo.Path.Resolved, err)
			}

			if !diskInfo.Backup {
				if err := disableBackup(diskInfo.Path.Resolved); err != nil {
					return fmt.Errorf("failed to disable backup for disk %s: %w", label, err)
				}
			} else if err := enableBackup(diskInfo.Path.Resolved); err != nil {
				return fmt.Errorf("failed to enable backup for disk %s: %w", label, err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to stat disk %s: %w", label, err)
		}

		if fsIdentify == nil {
			fsIdentify = fsidentifyAsync(size, diskInfo.Path.Resolved)
		}

		dev, err := newBlockDevice(diskInfo.Path.Resolved, diskInfo.ReadOnly, vz.DiskImageCachingModeCached, vz.DiskImageSynchronizationModeFsync)
		if err != nil {
			return fmt.Errorf("failed to create disk %s configuration: %w", label, err)
		}

		device := fmt.Sprintf("/dev/vd%c", 'a'+len(vm.storages))
		vm.storages = append(vm.storages, dev)
		mounted := false

		dm := diskMount{
			mountpoint: diskInfo.Mount,
			mountFunc: func(vm *VirtualMachine) error {
				result, err := fsIdentify()
				if err != nil {
					return fmt.Errorf("failed to identify filesystem: %w", err)
				}

				if result == nil || string(result.Type) != diskInfo.FS {
					progname := "mkfs." + diskInfo.FS
					args := append([]string{progname, "-L", label}, diskInfo.FormatOptions...)
					args = append(args, device)

					// mkfs
					cmd := rpc.Command{Path: "/sbin/" + progname, Args: args}

					if out, err := vm.Run(cmd); err != nil {
						return fmt.Errorf("failed to run mkfs: %w", err)
					} else if out.Exit != 0 {
						return fmt.Errorf("mkfs failed: %s", out.Output)
					}

					metrics := event.DiskMetrics{Total: uint64(size), Free: uint64(size)}
					vm.setDiskMetrics(diskInfo.Mount, metrics)
				} else if size != result.Size && size != 0 {
					log.Infof("resizing filesystem %s from %d to %d", diskInfo.Mount, result.Size, size)

					if diskInfo.FS == "ext4" {
						cmd := rpc.Command{Path: "/sbin/e2fsck", Args: []string{"e2fsck", "-f", "-y", device}}
						if out, err := vm.Run(cmd); err != nil {
							return fmt.Errorf("failed to run e2fsck: %w", err)
						} else if out.Exit != 0 {
							return fmt.Errorf("e2fsck failed: %s", out.Output)
						}

						cmd = rpc.Command{Path: "/usr/sbin/resize2fs", Args: []string{"resize2fs", device, fmt.Sprintf("%ds", size/512)}}

						if out, err := vm.Run(cmd); err != nil {
							return fmt.Errorf("failed to run resize2fs: %w", err)
						} else if out.Exit != 0 {
							return fmt.Errorf("resize2fs failed: %s", out.Output)
						}

						if stat.Size() > size {
							if err := setFileSize(diskInfo.Path.Resolved, size); err != nil {
								return fmt.Errorf("failed to truncate disk %s: %w", label, err)
							}
						}
					} else if diskInfo.FS == "btrfs" {
						// mount the filesystem
						if err := vm.Mount(device, diskInfo.Mount, diskInfo.FS, diskInfo.Options); err != nil {
							return fmt.Errorf("failed to mount %s: %w", diskInfo.Mount, err)
						}

						mounted = true
						cmd := rpc.Command{Path: "/sbin/btrfs", Args: []string{"btrfs", "filesystem", "resize", fmt.Sprintf("%d", size), diskInfo.Mount}}

						if out, err := vm.Run(cmd); err != nil {
							return fmt.Errorf("failed to run btrfs resize: %w", err)
						} else if out.Exit != 0 {
							return fmt.Errorf("btrfs resize failed: %s", out.Output)
						}

						if stat.Size() > size {
							if err := setFileSize(diskInfo.Path.Resolved, size); err != nil {
								return fmt.Errorf("failed to truncate disk %s: %w", label, err)
							}
						}
					}

					metrics := event.DiskMetrics{
						Total: uint64(size),
						Free:  util.Uint64(result.Free - (result.Size - size)),
					}

					vm.setDiskMetrics(diskInfo.Mount, metrics)
				} else {
					metrics := event.DiskMetrics{
						Total:     util.Uint64(result.Size),
						Free:      util.Uint64(result.Free),
						MaxFiles:  result.MaxFiles,
						FreeFiles: result.FreeFiles,
					}

					log.Infof("filesystem %s: %d/%d", diskInfo.Mount, metrics.Free, metrics.Total)

					vm.setDiskMetrics(diskInfo.Mount, metrics)
				}

				if !mounted {
					if err := vm.Mount(device, diskInfo.Mount, diskInfo.FS, diskInfo.Options); err != nil {
						return fmt.Errorf("failed to mount %s: %w", diskInfo.Mount, err)
					}
				}

				return nil
			},
		}

		vm.mounts = append(vm.mounts, dm)
	}

	if len(emptyDisks) > 0 {
		if err := vm.prepareAutoImages(emptyDisks); err != nil {
			return fmt.Errorf("failed to autosize images: %w", err)
		}
	}

	return nil
}

func (vm *VirtualMachine) setDiskMetrics(mountpoint string, metrics event.DiskMetrics) {
	vm.mutex.Lock()
	defer vm.mutex.Unlock()

	if vm.metrics.Disks == nil {
		vm.metrics.Disks = map[string]event.DiskMetrics{}
	}

	vm.metrics.Disks[mountpoint] = metrics
}

func setFileSize(path string, size int64) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)

	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", path, err)
	}

	defer file.Close()

	if err := file.Truncate(size); err != nil {
		return fmt.Errorf("failed to truncate file %s: %w", path, err)
	}

	return nil
}

func fsidentifyAsync(size int64, path string) func() (*disk.Filesystem, error) {
	return util.Await(func() (*disk.Filesystem, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %w", path, err)
		}

		defer file.Close()

		fs, err := disk.Identify(size, file)
		if err != nil {
			return nil, fmt.Errorf("failed to identify filesystem: %w", err)
		}

		return fs, nil
	})
}

type autoImages struct {
	images []*config.DiskImage
	stat   unix.Statfs_t
}

const (
	maxAutoSize      uint64 = 128 * 1024 * 1024 * 1024
	diskTotalPortion uint64 = 25 // 25% of the disk
	diskFreePortion  uint64 = 50 // 50% of free space
)

// for disks with no set size that do not exist, compute the size
func (vm *VirtualMachine) prepareAutoImages(images []*config.DiskImage) error {
	fs := map[unix.Fsid]*autoImages{}

	for _, image := range images {
		dir := filepath.Dir(image.Path.Resolved)
		stat := unix.Statfs_t{}

		if err := unix.Statfs(dir, &stat); err != nil {
			return fmt.Errorf("failed to statfs %s: %w", dir, err)
		}

		images := fs[stat.Fsid]
		if images == nil {
			images = &autoImages{stat: stat}
			fs[stat.Fsid] = images
		}

		images.images = append(images.images, image)
	}

	for _, images := range fs {
		total := images.stat.Blocks * uint64(images.stat.Bsize)
		free := images.stat.Bfree * uint64(images.stat.Bsize)
		space := util.Least(maxAutoSize, (total/diskTotalPortion)*100, (free/diskFreePortion)*100) / uint64(len(images.images))

		for _, image := range images.images {
			if err := setFileSize(image.Path.Resolved, util.Int64(space)); err != nil {
				return fmt.Errorf("failed to set file size for %s: %w", image.Path.Resolved, err)
			}
		}
	}

	return nil
}

const timeMachieBackupXattr = "com.apple.metadata:com_apple_backup_excludeItem"
const timeMachineBackupXattrValue = "com.apple.backupd"

func disableBackup(path string) error {
	return unix.Setxattr(path, timeMachieBackupXattr, []byte(timeMachineBackupXattrValue), 0)
}

func enableBackup(path string) error {
	return unix.Removexattr(path, timeMachieBackupXattr)
}
