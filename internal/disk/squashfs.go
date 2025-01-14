package disk

import (
	"encoding/binary"
	"fmt"
	"io"
)

const MinSizeSquashfs = 4

func IdentifySquashfs(file io.ReaderAt) (*Filesystem, error) {
	magic := make([]byte, 4)
	if _, err := file.ReadAt(magic, 0); err != nil {
		return nil, fmt.Errorf("failed to read paritition magic: %w", err)
	} else if binary.LittleEndian.Uint32(magic) == squashfsMagic {
		return &Filesystem{Type: FSsquash}, nil
	}

	return nil, nil
}
