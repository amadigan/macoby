package block

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/amadigan/macoby/internal/image"
	"github.com/amadigan/macoby/internal/util"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/google/uuid"
)

type TableType string

const (
	TableGPT TableType = "gpt"
	TableMBR TableType = "mbr"
	TableRaw TableType = "raw"
)

type Filesystem string

const (
	FSext    Filesystem = "ext4"
	FSxfs    Filesystem = "xfs"
	FSbtrfs  Filesystem = "btrfs"
	FSswap   Filesystem = "swap"
	FSfat    Filesystem = "vfat"
	FSsquash Filesystem = "squashfs"
)

type PartType string

const (
	PartTypeESP  PartType = "esp"
	PartTypeRoot PartType = "root"
	PartTypeSwap PartType = "swap"
)

const squashfsMagic uint32 = 0x73717368

type PartitionTable struct {
	Id             string
	Type           TableType
	Device         string
	DeviceName     string
	DiskSize       uint64
	AvailableSpace uint64
	Partitions     []*Partition
}

type Partition struct {
	Id             string
	FilesystemType Filesystem
	PartitionType  PartType
	TypeId         string
	FilesystemId   string
	Label          string
	Offset         uint64
	Blocks         uint64
	Size           uint64
	Device         string
}

func (t *PartitionTable) read(disk blockDevice) error {
	t.DiskSize = uint64(disk.len())

	log.Printf("Scanning for partition table on %s", t.Device)

	table := disk.table()

	var prevEnd uint64 = 34 * 512
	var space uint64

	t.Id = strings.ToLower(table.GUID)
	t.Type = TableGPT
	t.Partitions = make([]*Partition, 0, len(table.Partitions))

	for i, part := range table.Partitions {
		if part.Type == gpt.Unused {
			break
		}

		fromLast := uint64(part.GetStart()) - prevEnd

		log.Printf("Partition %d - prevEnd: %d, start: %d, fromLast: %d", i, prevEnd, part.GetStart(), fromLast)

		if fromLast > space {
			space = fromLast
		}

		prevEnd = util.Align(part.End*uint64(disk.blockSize()), 4096)

		guid := strings.ToLower(part.GUID)
		typeId := uuid.MustParse(string(part.Type))
		info := &Partition{Device: disk.partitionFile(i)}
		switch {

		case typeId == image.RootType:
			info.PartitionType = PartTypeRoot

			if contents, err := disk.partition(i); err == nil {
				defer contents.Close()
				if err := info.read(contents); err != nil {
					return fmt.Errorf("failed to read partitition %d in %s: %v", i, t.Device, err)
				}
			} else {
				return err
			}

		case part.Type == gpt.AppleBoot || part.Type == gpt.BIOSBoot || part.Type == gpt.LinuxDMCrypt || part.Type == gpt.LinuxLUKS || part.Type == gpt.LinuxLVM || part.Type == gpt.LinuxRAID:
		case part.Type == gpt.EFISystemPartition:
			info.PartitionType = PartTypeESP
			info.FilesystemType = FSfat
		case part.Type == gpt.LinuxSwap:
			info.PartitionType = PartTypeSwap
			info.FilesystemType = FSswap
		default:
			if contents, err := disk.partition(i); err == nil {
				defer contents.Close()
				if err := info.read(contents); err != nil {
					return fmt.Errorf("failed to read partitition %d in %s: %v", i, t.Device, err)
				}
			} else {
				return err
			}
		}

		info.Size = part.Size
		info.Id = guid
		info.TypeId = strings.ToLower(string(part.Type))
		info.Offset = part.Start
		info.Blocks = part.Size / uint64(disk.blockSize())

		t.Partitions = append(t.Partitions, info)
	}

	fromLast := t.DiskSize - prevEnd - (34 * 512)

	if fromLast > space {
		space = fromLast
	}

	space = (space / 4096) * 4096

	t.AvailableSpace = space

	return nil
}

func (t *PartitionTable) readRaw(file *os.File) error {
	t.Type = TableRaw

	part := &Partition{Device: t.Device}
	t.Partitions = []*Partition{part}
	t.DiskSize = part.Size

	if err := part.read(&systemPartition{File: file}); part.FilesystemType == "" {
		available := t.DiskSize - (40 * 512) - (34 * 512)

		available = (available / 4096) * 4096

		t.AvailableSpace = available

		return err
	}

	return nil
}

func (t *PartitionTable) ReadFile() error {
	t.DeviceName = t.Device

	dev, err := openFileBlockDevice(t.Device)

	if err != nil {
		return err
	}

	defer dev.Close()

	if dev.table() == nil {
		file, err := dev.file()

		if err != nil {
			return err
		}

		return t.readRaw(file)
	} else {
		return t.read(dev)
	}
}

func (t *PartitionTable) ReadDevice() error {
	deviceName := t.Device

	if index := strings.LastIndex(deviceName, "/"); index >= 0 {
		deviceName = deviceName[index+1:]
	}

	t.Device = "/dev/" + deviceName
	t.DeviceName = deviceName

	dev, err := openSystemBlockDevice(t.DeviceName)

	if err != nil {
		return err
	}

	defer dev.Close()

	if dev.table() == nil {
		file, err := dev.file()

		if err != nil {
			return err
		}

		return t.readRaw(file)
	} else {
		return t.read(dev)
	}
}

func (p *Partition) read(f blockPartition) error {
	p.FilesystemId = ""
	p.FilesystemType = ""
	p.Label = ""

	size, err := f.len()

	if err != nil {
		return fmt.Errorf("failed to seek to end of partition: %v", err)
	}

	p.Size = uint64(size)

	if size >= 4 {
		magic := make([]byte, 4)
		if _, err := f.ReadAt(magic, 0); err != nil {
			return fmt.Errorf("failed to read paritition magic: %v", err)
		} else if binary.LittleEndian.Uint32(magic) == squashfsMagic {
			p.FilesystemType = FSsquash

			return nil
		}
	}

	if size > 1024+0x88 {
		bs := make([]byte, 0x88)
		if _, err = f.ReadAt(bs, 1024); err != nil {
			return fmt.Errorf("failed to read ext superblock from partition %s: %v", p.Device, err)
		}

		if bs[0x38] == 0x53 && bs[0x39] == 0xEF {
			// this is an ext2/3/4 filesystem
			p.FilesystemType = FSext

			id, err := uuid.FromBytes(bs[0x68:0x78])

			if err != nil {
				return err
			}

			p.FilesystemId = id.String()

			label := string(bs[0x78:0x88])

			if end := strings.IndexRune(label, 0); end >= 0 {
				label = label[:end]
			}

			p.Label = label

			return nil
		}
	}

	if size > 0x10000+1000 {
		bs := make([]byte, 1000)

		if _, err := f.ReadAt(bs, 0x10000); err != nil {
			return err
		}

		if string(bs[0x40:0x48]) == "_BHRfS_M" {
			p.FilesystemType = FSbtrfs

			id, err := uuid.FromBytes(bs[0x20:0x30])

			if err != nil {
				return err
			}

			p.FilesystemId = id.String()

			label := string(bs[0x12b:0x22b])

			if end := strings.IndexRune(label, 0); end >= 0 {
				label = label[:end]
			}

			p.Label = label

			return nil
		}
	}

	if size > 120 {
		bs := make([]byte, 120)

		if _, err := f.ReadAt(bs, 0); err != nil {
			return err
		}

		if string(bs[0:4]) == "XFSB" {
			p.FilesystemType = FSxfs

			id, err := uuid.FromBytes(bs[32:48])

			if err != nil {
				return err
			}

			p.FilesystemId = id.String()

			label := string(bs[108:120])

			if end := strings.IndexRune(label, 0); end >= 0 {
				label = label[:end]
			}

			p.Label = label

			return nil
		}
	}

	return nil
}

func parseIntFile(file string) (uint64, error) {
	bs, err := os.ReadFile(file)

	if err != nil {
		return 0, err
	}

	str := strings.TrimSpace(string(bs))

	return strconv.ParseUint(str, 10, 64)
}

func reverseBytes(bs []byte) {
	for i := 0; i < len(bs)/2; i++ {
		bs[i], bs[len(bs)-1-i] = bs[len(bs)-1-i], bs[i]
	}
}
