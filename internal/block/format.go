package block

import (
	"fmt"
	"log"
	"os/exec"

	"github.com/amadigan/macoby/internal/util"
	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/google/uuid"
)

type FormatSpec struct {
	PartitionId   uuid.UUID
	FilesystemId  uuid.UUID
	PartitionType uuid.UUID
	DiskId        uuid.UUID
	Label         string
}

func (p *PartitionTable) Format(spec FormatSpec) (*Partition, error) {

	if p.Type == TableMBR {
		return nil, fmt.Errorf("Formatting MBR disks is not supported (device: %s)", p.Device)
	}

	if err := p.createPartition(spec); err != nil {
		return nil, err
	}

	if err := p.ReadDevice(); err != nil {
		return nil, err
	}

	log.Printf(p.String())

	var partition *Partition

	for _, part := range p.Partitions {
		if part.Id == spec.PartitionId.String() {
			partition = part
			break
		}
	}

	if partition == nil {
		return nil, fmt.Errorf("Unable to find partitition %s after re-partitioning", spec.PartitionId.String())
	}

	if err := FormatRaw(partition.Device, spec.Label, spec.FilesystemId); err != nil {
		return partition, err
	}

	return partition, partition.read(nil) //TODO
}

func (p *PartitionTable) createPartition(spec FormatSpec) error {
	d, err := diskfs.Open(p.Device)

	if err != nil {
		return fmt.Errorf("Unable to open device %s: %v", p.Device, err)
	}

	log.Printf("Disk size: %d", d.Size)

	defer d.Close()

	var table *gpt.Table

	partNum := 0
	lastPart := -1
	var size int64 = -1

	if p.Type == TableGPT {
		iTable, err := d.GetPartitionTable()

		if err != nil {
			return fmt.Errorf("Unable to read GPT table from %s: %v", p.Device, err)
		}

		table = iTable.(*gpt.Table)

		var prevEnd int64 = util.Align(34*int64(table.LogicalSectorSize), 4096)

		for index, part := range table.Partitions {
			if part.Type == gpt.Unused {
				break
			}

			lastPart = index

			newSize := ((part.GetStart() - prevEnd) / 4096) * 4096

			if newSize > size {
				partNum = index + 1
				size = newSize
			}

			prevEnd = util.Align(int64(part.End+1)*int64(d.LogicalBlocksize), 4096)
		}

		fromLast := d.Size - prevEnd - (34 * 512)

		if fromLast > size {
			size = (fromLast / 4096) * 4096
		}
	} else {
		table = &gpt.Table{ProtectiveMBR: true}

		if spec.DiskId != uuid.Nil {
			table.GUID = spec.DiskId.String()
		}

		if size, err := parseIntFile("/sys/block/" + p.DeviceName + "/queue/physical_block_size"); err == nil {
			table.PhysicalSectorSize = int(size)
		} else {
			log.Printf("Unable to read physical sector size for device %s (%v), defaulting to 512", p.DeviceName, err)
			table.PhysicalSectorSize = 512
		}

		if size, err := parseIntFile("/sys/block/" + p.DeviceName + "/queue/logical_block_size"); err == nil {
			table.LogicalSectorSize = int(size)
		} else {
			log.Printf("Unable to read logical sector size for device %s (%v), defaulting to 512", p.DeviceName, err)
			table.LogicalSectorSize = 512
		}
	}

	newPartition := &gpt.Partition{
		Name: spec.Label,
	}

	if spec.PartitionId == uuid.Nil {
		spec.PartitionId = uuid.New()
	}

	newPartition.GUID = spec.PartitionId.String()

	if spec.PartitionType != uuid.Nil {
		newPartition.Type = gpt.Type(spec.PartitionType.String())
	} else {
		newPartition.Type = gpt.LinuxFilesystem
	}

	startByte := util.Align(34*int64(table.LogicalSectorSize), 4096)

	if partNum > 0 {
		startByte = util.Align(int64(table.Partitions[partNum-1].End+1)*int64(d.LogicalBlocksize), 4096)
	}

	newPartition.Start = uint64(startByte) / uint64(table.LogicalSectorSize)

	var endByte int64

	if partNum < lastPart {
		endByte = table.Partitions[partNum+1].GetStart()
	} else {
		endByte = d.Size - int64(32*table.LogicalSectorSize)
	}

	endByte = startByte + ((endByte-startByte)/4096)*4096
	log.Printf("endByte: %d", endByte)
	newPartition.End = uint64(endByte/int64(table.LogicalSectorSize) - 1)

	if partNum < lastPart {
		if lastPart+1 < len(table.Partitions) {
			table.Partitions = append(table.Partitions, nil)
		}

		copy(table.Partitions[partNum+1:], table.Partitions[partNum:lastPart+1])
		table.Partitions[partNum] = newPartition
	} else if partNum+1 > len(table.Partitions) {
		table.Partitions = append(table.Partitions, newPartition)
	} else {
		table.Partitions[partNum] = newPartition
	}

	log.Printf("Partitioning: %#v", table)
	log.Printf("Partitioning: %#v", newPartition)
	log.Printf("Partition Start: %d, End: %d", newPartition.Start, newPartition.End)

	if err := d.Partition(table); err != nil {
		return fmt.Errorf("Unable to partition %s: %v", p.Device, err)
	}

	return nil
}

func FormatRaw(device string, label string, id uuid.UUID) error {
	args := make([]string, 3, 8)

	args[0] = "mkfs.ext4"
	args[1] = "-q"
	args[2] = "-F"

	if label != "" {
		args = append(args, "-L", label)
	}

	if id != uuid.Nil {
		args = append(args, "-U", id.String())
	}

	args = append(args, device)

	cmd := exec.Command("/sbin/mke2fs")

	cmd.Args = args

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Failed to format %s - %v: %s", device, err, out)
	}

	return nil
}
