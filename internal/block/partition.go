package block

import (
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"

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
	FSnone   Filesystem = ""
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
