package boot

import (
	"errors"

	"github.com/amadigan/macoby/internal/block"
	"github.com/google/uuid"
)

func FindPartition(spec block.FormatSpec, fsType block.Filesystem, minSize uint64, mustMatch bool, devices *block.DeviceTable) *block.Partition {
	rules := 0

	if spec.DiskId != uuid.Nil {
		rules = rules + 1
	}

	if spec.FilesystemId != uuid.Nil {
		rules = rules + 1
	}

	if spec.Label != "" {
		rules = rules + 1
	}

	if spec.PartitionId != uuid.Nil {
		rules = rules + 1
	}

	if spec.PartitionType != uuid.Nil {
		rules = rules + 1
	}

	if fsType != "" {
		rules = rules + 1
	}

	matched := -1
	var match *block.Partition

	for _, disk := range devices.Devices {
		for _, part := range disk.Partitions {
			if part.Size >= minSize && (part.FilesystemType == block.FSbtrfs || part.FilesystemType == block.FSxfs || part.FilesystemType == block.FSext) {
				thisMatch := 0

				if spec.DiskId != uuid.Nil && disk.Id == spec.DiskId.String() {
					thisMatch = thisMatch + 1
				}

				if spec.FilesystemId != uuid.Nil && part.FilesystemId == spec.FilesystemId.String() {
					thisMatch = thisMatch + 1
				}

				if spec.Label != "" && part.Label == spec.Label {
					thisMatch = thisMatch + 1
				}

				if spec.PartitionId != uuid.Nil && part.Id == spec.PartitionId.String() {
					thisMatch = thisMatch + 1
				}

				if spec.PartitionType != uuid.Nil && part.TypeId == spec.PartitionId.String() {
					thisMatch = thisMatch + 1
				}

				if fsType != "" && part.FilesystemType == fsType {
					thisMatch = thisMatch + 1
				}

				if (!mustMatch || rules == thisMatch) && thisMatch >= matched && (match == nil || part.Size > match.Size) {
					matched = thisMatch
					match = part
				}
			}
		}
	}

	return match
}

func AutoFormat(spec block.FormatSpec, minSize uint64, mountpoint string, devices *block.DeviceTable) error {
	var disk *block.PartitionTable

	for _, d := range devices.Devices {
		if d.AvailableSpace >= minSize && (disk == nil || d.AvailableSpace > disk.AvailableSpace) {
			disk = d
		}
	}

	if disk == nil {
		return errors.New("No candidates disks to format")
	}

	partition, err := disk.Format(spec)

	if err != nil {
		return err
	}

	return Mount(partition, mountpoint)
}
