package boot

import (
	"fmt"

	"github.com/amadigan/macoby/internal/block"
)

type mounter func(source string, target string, fstype string, flags uintptr, data string) error

var sysMount mounter

func Mount(partition *block.Partition, mountpoint string) error {
	if sysMount == nil {
		return fmt.Errorf("Mount not initialized")
	}

	if err := sysMount(partition.Device, mountpoint, string(partition.PartitionType), 0, ""); err != nil {
		return fmt.Errorf("Failed to mount %s as %s on %s: %v", partition.Device, partition.PartitionType, mountpoint, err)
	}

	return nil
}
