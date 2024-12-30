package block

import (
	"fmt"
	"os"
	"regexp"
)

var devRegex *regexp.Regexp = regexp.MustCompile("^vd[a-z]+$")

type DeviceTable struct {
	Devices map[string]*PartitionTable
}

func ScanDevices() (*DeviceTable, error) {
	infos, err := os.ReadDir("/sys/block")

	if err != nil {
		return nil, fmt.Errorf("Unable to list /sys/block: %v", err)
	}

	devices := &DeviceTable{Devices: make(map[string]*PartitionTable)}

	for _, info := range infos {
		name := info.Name()
		if devRegex.MatchString(name) {
			table := &PartitionTable{Device: name}

			if err := table.ReadDevice(); err != nil {
				return nil, fmt.Errorf("Unable to read device %s: %v", name, err)
			}
			devices.Devices[name] = table
		}
	}

	return devices, nil
}

func (d *DeviceTable) String() string {
	result := ""
	for key, device := range d.Devices {
		partitions := ""

		for i, part := range device.Partitions {
			partitions = partitions + fmt.Sprintf("\tPartition %d:\n"+
				"\t\tID: %s\n"+
				"\t\tType: %s\n"+
				"\t\tType ID: %s\n"+
				"\t\tFilesystem Type: %s\n"+
				"\t\tFilesystem ID: %s\n"+
				"\t\tLabel: %s\n"+
				"\t\tSize: %d\n"+
				"\t\tDevice: %s\n", i, part.Id, part.PartitionType, part.TypeId, part.FilesystemType, part.FilesystemId, part.Label, part.Size, part.Device)
		}

		result = result + fmt.Sprintf("%s:\n"+
			"\tName: %s\n"+
			"\tID: %s\n"+
			"\tDevice: %s\n"+
			"\tType: %s\n"+
			"\tSize: %d\n"+
			"\tAvailable: %d\n"+
			"%s\n", key, device.DeviceName, device.Id, device.Device, device.Type, device.DiskSize, device.AvailableSpace, partitions)
	}

	return result

}

func (device *PartitionTable) String() string {
	partitions := ""

	for i, part := range device.Partitions {
		partitions = partitions + fmt.Sprintf("\tPartition %d:\n"+
			"\tID: %s\n"+
			"\tType: %s\n"+
			"\tType ID: %s\n"+
			"\tFilesystem Type: %s\n"+
			"\tFilesystem ID: %s\n"+
			"\tLabel: %s\n"+
			"\tSize: %d\n"+
			"\tDevice: %s\n", i, part.Id, part.PartitionType, part.TypeId, part.FilesystemType, part.FilesystemId, part.Label, part.Size, part.Device)
	}

	return fmt.Sprintf(
		"Name: %s\n"+
			"ID: %s\n"+
			"Device: %s\n"+
			"Type: %s\n"+
			"Size: %d\n"+
			"Available: %d\n"+
			"%s\n", device.DeviceName, device.Id, device.Device, device.Type, device.DiskSize, device.AvailableSpace, partitions)
}
