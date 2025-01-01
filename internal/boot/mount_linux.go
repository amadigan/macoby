package boot

import (
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/amadigan/macoby/internal/util"
	"golang.org/x/sys/unix"
)

const OverlayPath = "/overlay"

func init() {
	sysMount = unix.Mount
}

func mount(fstype, device, mountpoint string) error {
	return unix.Mount(device, mountpoint, fstype, 0, "")
}

func MountCoreFS() {
	util.Must(MountProc())
	util.Must(MountSys())
	util.Must(MountCgroup())
}

func MountTmp(device string, mountpoint string, size int64) error {
	if err := unix.Mount(device, mountpoint, "tmpfs", unix.MS_NOEXEC|unix.MS_NOSYMFOLLOW|unix.MS_NOATIME, fmt.Sprintf("uid=0,gid=0,mode=0755,size=%d", size)); err != nil {
		return fmt.Errorf("Failed to mount %s tmpfs on %s: %v", device, mountpoint, err)
	}

	return nil
}

func MountProc() error {
	if err := unix.Mount("proc", "/proc", "proc", unix.MS_NOSUID|unix.MS_STRICTATIME, ""); err != nil {
		return fmt.Errorf("Failed to mount /proc: %v", err)
	}

	return nil
}

func MountSys() error {
	if err := unix.Mount("sysfs", "/sys", "sysfs", unix.MS_NOEXEC|unix.MS_NOATIME, ""); err != nil {
		return fmt.Errorf("Failed to mount /sys: %v", err)
	}

	return nil
}

// MountCgroup ... mount unified cgroup2 hierarchy, supported by Docker starting with 20.10
func MountCgroup() error {
	// Other early boot tasks: mounting /proc, /sys, etc.
	if err := os.MkdirAll("/sys/fs/cgroup", 0755); err != nil {
		log.Fatal(err)
	}

	// Mount cgroup v2 at /sys/fs/cgroup
	// (The "none" or "cgroup2" type typically works; you can test "cgroup2" on some kernels.)
	if err := syscall.Mount("cgroup2", "/sys/fs/cgroup", "cgroup2", 0, ""); err != nil {
		log.Fatalf("Failed to mount cgroup2: %v", err)
	}

	// Optionally enable all controllers that are available
	// (You might not have all of these, so read cgroup.controllers first.)
	controllers, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers")
	if err == nil {
		// e.g. if cgroup.controllers => "cpuset cpu io memory hugetlb pids"
		// we want to write "+cpu +io +memory +pids ..." to /sys/fs/cgroup/cgroup.subtree_control
		enabled := []string{}
		for _, c := range strings.Fields(string(controllers)) {
			enabled = append(enabled, "+"+c)
		}
		if len(enabled) > 0 {
			cmd := strings.Join(enabled, " ")
			if err := os.WriteFile("/sys/fs/cgroup/cgroup.subtree_control", []byte(cmd), 0644); err != nil {
				fmt.Printf("Warning: cannot enable controllers: %v\n", err)
			}
		}
	}

	return nil
}

func OverlayRoot() error {
	if err := MountTmp("tmpfs", OverlayPath, 1024*1024); err != nil {
		return fmt.Errorf("Failed to mount overlay root: %v", err)
	}

	if err := os.MkdirAll(path.Join(OverlayPath, "upper"), 0755); err != nil {
		return fmt.Errorf("Failed to create upper overlay directory: %w", err)
	}

	if err := os.MkdirAll(path.Join(OverlayPath, "work"), 0755); err != nil {
		return fmt.Errorf("Failed to create work overlay directory: %w", err)
	}

	if err := os.MkdirAll(path.Join(OverlayPath, "oldroot"), 0755); err != nil {
		return fmt.Errorf("Failed to create merged overlay directory: %w", err)
	}

	if err := os.MkdirAll(path.Join(OverlayPath, "newroot"), 0755); err != nil {
		return fmt.Errorf("Failed to create merged overlay directory: %w", err)
	}

	// mount oldroot (/dev/vda) to lower
	if err := unix.Mount("/dev/vda", path.Join(OverlayPath, "oldroot"), "squashfs", 0, "ro"); err != nil {
		return fmt.Errorf("Failed to mount lower overlay: %w", err)
	}

	mountOpts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", path.Join(OverlayPath, "oldroot"),
		path.Join(OverlayPath, "upper"), path.Join(OverlayPath, "work"))

	if err := unix.Mount("overlay", "/overlay/newroot", "overlay", 0, mountOpts); err != nil {
		return fmt.Errorf("Failed to mount overlay: %w", err)
	}

	if err := os.MkdirAll("/overlay/newroot/oldroot", 0755); err != nil {
		return fmt.Errorf("Failed to create oldroot directory: %w", err)
	}

	if err := unix.PivotRoot("/overlay/newroot", "/overlay/newroot/oldroot"); err != nil {
		return fmt.Errorf("Failed to pivot root: %w", err)
	}

	if err := unix.Mount("devtmpfs", "/dev", "devtmpfs", unix.MS_NOSUID|unix.MS_STRICTATIME, ""); err != nil {
		return fmt.Errorf("Failed to mount /dev: %w", err)
	}

	if err := unix.Unmount("/oldroot/dev", 0); err != nil {
		return fmt.Errorf("Failed to unmount /oldroot/dev: %w", err)
	}

	return nil
}
