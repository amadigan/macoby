package block

import (
	"fmt"
	"io"
	"os"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/partition/gpt"
)

type blockDevice interface {
	partitionCount() int
	partition(int) (blockPartition, error)
	partitionFile(int) string
	table() *gpt.Table
	len() int64
	file() (*os.File, error)
	blockSize() int64
	io.Closer
}

type blockPartition interface {
	io.Closer
	io.Reader
	io.ReaderAt
	io.Seeker
	len() (int64, error)
}

type systemBlockDevice struct {
	deviceFile string
	partMap    map[int]string
	partTable  *gpt.Table
	disk       *disk.Disk
}

func openSystemBlockDevice(deviceName string) (blockDevice, error) {
	deviceFile := "/dev/" + deviceName
	disk, err := diskfs.Open(deviceFile, diskfs.WithOpenMode(diskfs.ReadOnly))

	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %v", deviceFile, err)
	}

	disk.ReReadPartitionTable()

	partMap := make(map[int]string)

	infos, err := os.ReadDir("/sys/block/" + deviceName)

	if err != nil {
		disk.Close()
		return nil, fmt.Errorf("unable to list /sys/block/%s: %v", deviceName, err)
	}

	for _, info := range infos {
		if info.IsDir() {
			if partNum, err := parseIntFile("/sys/block/" + deviceName + "/" + info.Name() + "/partition"); err == nil {
				partMap[int(partNum)-1] = info.Name()
			} else if !os.IsNotExist(err) {
				disk.Close()
				return nil, fmt.Errorf("unable to read /sys/block/%s/%s: %v", deviceName, info.Name(), err)
			}
		}
	}

	rv := &systemBlockDevice{
		deviceFile: deviceFile,
		partMap:    partMap,
		disk:       disk,
	}

	if len(partMap) > 0 {
		table, err := disk.GetPartitionTable()

		if err != nil {
			disk.Close()
			return nil, err
		}

		if table.Type() != "gpt" {
			disk.Close()
			return nil, fmt.Errorf("partition type %s is not supported", table.Type())
		}

		rv.partTable = table.(*gpt.Table)
	}

	return rv, err
}

func (d *systemBlockDevice) blockSize() int64 {
	return d.disk.LogicalBlocksize
}

func (d *systemBlockDevice) len() int64 {
	return d.disk.Size
}

func (d *systemBlockDevice) file() (*os.File, error) {
	return d.disk.Backend.Sys()
}

func (d *systemBlockDevice) Close() error {
	return d.disk.Close()
}

func (d *systemBlockDevice) partitionCount() int {
	return len(d.partMap)
}

func (d *systemBlockDevice) partitionFile(index int) string {
	if dev, ok := d.partMap[index]; ok {
		return "/dev/" + dev
	}

	return ""
}

func (d *systemBlockDevice) partition(index int) (blockPartition, error) {
	if dev, ok := d.partMap[index]; ok {
		f, err := os.Open("/dev/" + dev)

		return &systemPartition{File: f}, err
	}

	return nil, os.ErrNotExist
}

func (d *systemBlockDevice) table() *gpt.Table {
	return d.partTable
}

type systemPartition struct {
	*os.File
}

func (s *systemPartition) len() (int64, error) {
	off, err := s.Seek(0, io.SeekCurrent)

	if err != nil {
		return 0, err
	}

	size, err := s.Seek(0, io.SeekEnd)

	if err != nil {
		return size, err
	}

	_, err = s.Seek(off, io.SeekStart)

	return size, err
}

type fileBlockDevice struct {
	disk      *disk.Disk
	partTable *gpt.Table
}

func openFileBlockDevice(name string) (blockDevice, error) {
	disk, err := diskfs.Open(name, diskfs.WithOpenMode(diskfs.ReadOnly))

	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %v", name, err)
	}

	table, err := disk.GetPartitionTable()

	rv := fileBlockDevice{disk: disk}

	if err == nil && table != nil {
		if table.Type() == "gpt" {
			rv.partTable = table.(*gpt.Table)
		} else {
			disk.Close()
			return nil, fmt.Errorf("partition type %s is not supported", table.Type())
		}
	}

	return &rv, nil
}

func (d *fileBlockDevice) partitionFile(int) string {
	return ""
}

func (d *fileBlockDevice) blockSize() int64 {
	return d.disk.LogicalBlocksize
}

func (d *fileBlockDevice) table() *gpt.Table {
	return d.partTable
}

func (d *fileBlockDevice) len() int64 {
	return d.disk.Size
}

func (d *fileBlockDevice) file() (*os.File, error) {
	return d.disk.Backend.Sys()
}

func (d *fileBlockDevice) Close() error {
	return d.disk.Close()
}

func (d *fileBlockDevice) partitionCount() int {
	return len(d.partTable.Partitions)
}

type filePartition struct {
	start int64
	end   int64
	pos   int64
	file  *os.File
}

func (f *filePartition) Close() error {
	return nil
}

func (f *filePartition) Read(p []byte) (int, error) {
	if f.pos >= f.end {
		return 0, io.EOF
	}

	if int64(len(p))+f.pos > f.end {
		p = p[0:(f.end - f.pos)]
	}

	r, err := f.file.ReadAt(p, f.pos)
	f.pos += int64(r)
	return r, err
}

func (f *filePartition) ReadAt(p []byte, off int64) (int, error) {
	off += f.start
	if off >= f.end {
		return 0, io.EOF
	}

	if int64(len(p))+off > f.end {
		p = p[0:(f.end - off)]
	}

	return f.file.ReadAt(p, off)
}

func (f *filePartition) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		offset += f.start
	case io.SeekCurrent:
		offset += f.pos
	case io.SeekEnd:
		offset += f.end
	default:
		return f.pos, os.ErrInvalid
	}

	if offset < f.start || offset > f.end {
		return f.pos, os.ErrInvalid
	}

	f.pos = offset
	return f.pos, nil
}

func (f *filePartition) len() (int64, error) {
	return f.end - f.start, nil
}

func (d *fileBlockDevice) partition(index int) (blockPartition, error) {
	if index > len(d.partTable.Partitions) {
		return nil, os.ErrNotExist
	}

	part := d.partTable.Partitions[index]

	if part.Type == gpt.Unused {
		return nil, os.ErrNotExist
	}

	file, err := d.file()

	if err != nil {
		return nil, err
	}

	return &filePartition{
		start: part.GetStart(),
		end:   part.GetStart() + part.GetSize(),
		file:  file,
	}, nil
}
