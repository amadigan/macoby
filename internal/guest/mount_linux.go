package guest

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

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
