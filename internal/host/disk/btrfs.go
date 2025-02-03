package disk

import (
	"encoding/binary"
	"fmt"
	"io"
)

const MinSizeBtrfs = 0x10000 + 1000

func IdentifyBtrfs(file io.ReaderAt) (*Filesystem, error) {
	superblock := make([]byte, 1000)

	if _, err := file.ReadAt(superblock, 0x10000); err != nil {
		return nil, fmt.Errorf("failed to read superblock: %w", err)
	}

	if string(superblock[0x40:0x48]) != "_BHRfS_M" {
		return nil, nil
	}

	p := Filesystem{
		Type:  FSbtrfs,
		Id:    readFilesystemId(superblock, 0x20),
		Label: readLabel(superblock[0x12B:0x22B]),
		//nolint:gosec
		Size: int64(binary.LittleEndian.Uint64(superblock[0x70:0x78])),
	}

	return &p, nil
}
