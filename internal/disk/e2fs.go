package disk

import (
	"encoding/binary"
	"fmt"
	"io"
)

const MinSizeExt = 1024 + 0x88

func IdentifyExt(file io.ReaderAt) (*Filesystem, error) {
	bs := make([]byte, 0x88)
	if _, err := file.ReadAt(bs, 1024); err != nil {
		return nil, fmt.Errorf("failed to read superblock: %w", err)
	}

	if bs[0x38] != 0x53 || bs[0x39] != 0xEF {
		return nil, nil
	}

	p := Filesystem{
		Type:  FSext,
		Id:    readFilesystemId(bs, 0x68),
		Label: readLabel(bs[0x78:0x88]),
	}

	sBlocksCount := binary.LittleEndian.Uint32(bs[0x4:0x8])
	sLogBlockSize := binary.LittleEndian.Uint32(bs[0x18:0x1C])

	blockSize := int64(1024 << sLogBlockSize)

	p.Size = int64(sBlocksCount) * blockSize

	return &p, nil
}
