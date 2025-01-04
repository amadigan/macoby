package guest

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

var safeFilesystems = map[string]bool{
	"proc":       true,
	"sysfs":      true,
	"tmpfs":      true,
	"devtmpfs":   true,
	"securityfs": true,
	"debugfs":    true,
	"tracefs":    true,
	"pstore":     true,
	"cgroup":     true,
	"bpf":        true,
	"hugetlbfs":  true,
	"overlayfs":  true,
	"squashfs":   true,
	"iso9660":    true,
	"romfs":      true,
	"cramfs":     true,
	"nsfs":       true,
	"aufs":       true,
	"shiftfs":    true,
	"rpc_pipefs": true,
	"nfsd":       true,
	"fusectl":    true,
	"mqueue":     true,
	"efivarfs":   true,
}

var safeMounts = map[string]bool{
	"/proc": true,
	"/sys":  true,
	"/dev":  true,
	"/":     true,
}

func UnmountAll(ctx context.Context) error {
	file, err := os.Open("/proc/self/mounts")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening /proc/self/mounts: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// Initialize slice of unmount targets
	var targets []string

	// Read file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Parse line into fields (space-separated)
		fields := strings.Fields(line)
		if len(fields) < 3 {
			// Invalid line, skip
			continue
		}

		// Extract mount point and filesystem type
		mountPoint := fields[1]
		filesystem := fields[2]

		// Check if the mount point or filesystem is considered "safe"
		if safeMounts[mountPoint] || safeFilesystems[filesystem] {
			// Skip safe mounts/filesystems
			continue
		}

		// Add to targets for unmounting
		targets = append(targets, mountPoint)
	}

	// Check for errors while reading
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading /proc/self/mounts: %v\n", err)
		os.Exit(1)
	}

	for i := len(targets) - 1; i >= 0; i-- {
		if err := unix.Unmount(targets[i], 0); err != nil {
			fmt.Fprintf(os.Stderr, "Error unmounting %s: %v\n", targets[i], err)
			os.Exit(1)
		}
	}

	return nil
}

func mkext4(device string, options []string) error {
	args := []string{"mkfs.ext4", "-q", "-F"}
	args = append(args, options...)
	args = append(args, device)

	cmd := exec.Command("/sbin/mke2fs")
	cmd.Args = args

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Failed to format %s - %v: %s", device, err, out)
	}

	return nil
}

func mkbtrfs(device string, options []string) error {
	args := []string{"mkfs.btrfs", "-q", "-f"}
	args = append(args, options...)
	args = append(args, device)

	cmd := exec.Command("/sbin/mkfs.btrfs")
	cmd.Args = args

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Failed to format %s - %v: %s", device, err, out)
	}

	return nil
}

func mkswap(device string, options []string) error {
	args := []string{"mkswap"}
	args = append(args, options...)
	args = append(args, device)

	cmd := exec.Command("/bin/busybox")
	cmd.Args = args

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Failed to format %s - %v: %s", device, err, out)
	}

	return nil
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

func OverlayRoot(size int64) error {
	if err := MountTmp("tmpfs", "/mnt", size); err != nil {
		return fmt.Errorf("Failed to mount tmpfs: %w", err)
	}

	if err := os.MkdirAll("/mnt/upper", 0755); err != nil {
		return fmt.Errorf("Failed to create upper overlay directory: %w", err)
	}

	if err := os.MkdirAll("/mnt/work", 0755); err != nil {
		return fmt.Errorf("Failed to create work overlay directory: %w", err)
	}

	if err := os.MkdirAll("/mnt/newroot", 0755); err != nil {
		return fmt.Errorf("Failed to create merged overlay directory: %w", err)
	}

	if err := unix.Mount("overlay", "/mnt/newroot", "overlay", 0, "lowerdir=/,upperdir=/mnt/upper,workdir=/mnt/work"); err != nil {
		return fmt.Errorf("Failed to mount overlay: %w", err)
	}

	// mount the newroot to /, and then mount the oldroot over it
	if err := unix.PivotRoot("/mnt/newroot", "/mnt/newroot"); err != nil {
		return fmt.Errorf("Failed to pivot root: %w", err)
	}

	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("Failed to change directory: %w", err)
	}

	// unmount the oldroot to uncover the newroot
	if err := unix.Unmount("/", unix.MNT_DETACH); err != nil {
		return fmt.Errorf("Failed to unmount /: %w", err)
	}

	if err := unix.Mount("devtmpfs", "/dev", "devtmpfs", unix.MS_NOSUID|unix.MS_STRICTATIME, ""); err != nil {
		return fmt.Errorf("Failed to mount /dev: %w", err)
	}

	return nil
}
