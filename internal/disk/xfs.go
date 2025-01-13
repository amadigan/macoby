package disk

import (
	"encoding/binary"
	"io"
)

const MinSizeXfs = 512

func IdentifyXfs(file io.ReaderAt) (*Filesystem, error) {
	superblock := make([]byte, 512)

	if _, err := file.ReadAt(superblock, 0); err != nil {
		return nil, err
	}

	if string(superblock[0x0:0x4]) != "XFSB" {
		return nil, nil
	}

	p := Filesystem{
		Type:  FSxfs,
		Id:    readFilesystemId(superblock, 0x20),
		Label: readLabel(superblock[0x6C:0x78]),
	}

	sbBlocksize := binary.LittleEndian.Uint32(superblock[0x4:0x8])
	//nolint:gosec
	sbDblocks := int64(binary.LittleEndian.Uint64(superblock[0x10:0x18]))

	p.Size = int64(sbBlocksize) * sbDblocks

	return &p, nil
}
