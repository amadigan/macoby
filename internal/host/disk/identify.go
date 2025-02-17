package disk

import (
	"bytes"
	"io"

	"github.com/google/uuid"
)

type FSType string

const (
	FSnone   FSType = ""
	FSext    FSType = "ext4"
	FSxfs    FSType = "xfs"
	FSbtrfs  FSType = "btrfs"
	FSswap   FSType = "swap"
	FSfat    FSType = "vfat"
	FSsquash FSType = "squashfs"
)

const squashfsMagic uint32 = 0x73717368

type Filesystem struct {
	Type      FSType
	Id        uuid.UUID
	Label     string
	Size      int64 // in bytes
	Free      int64
	MaxFiles  uint64
	FreeFiles uint64
}

func Identify(size int64, f io.ReaderAt) (*Filesystem, error) {
	if size > MinSizeSquashfs {
		if rv, err := IdentifySquashfs(f); rv != nil || err != nil {
			return rv, err
		}
	}

	if size > MinSizeExt {
		if rv, err := IdentifyExt(f); rv != nil || err != nil {
			return rv, err
		}
	}

	if size > 0x10000+1000 {
		if rv, err := IdentifyBtrfs(f); rv != nil || err != nil {
			return rv, err
		}
	}

	if size > MinSizeXfs {
		if rv, err := IdentifyXfs(f); rv != nil || err != nil {
			return rv, err
		}
	}

	return nil, nil
}

func readFilesystemId(bs []byte, offset int) uuid.UUID {
	var id uuid.UUID
	copy(id[:], bs[offset:offset+16])

	return id
}

func readLabel(bs []byte) string {
	if end := bytes.IndexByte(bs, 0); end >= 0 {
		return string(bs[:end])
	}

	return string(bs)
}
