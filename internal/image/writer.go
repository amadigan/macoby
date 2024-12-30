package image

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/amadigan/macoby/internal/util"
	"github.com/diskfs/go-diskfs/partition/gpt"
)

type imageWriter struct {
	table *gpt.Table
	path  string
	file  *os.File
}

type partitionWriter struct {
	image  *imageWriter
	offset int64
	length int64
	pos    int64
}

type externalPartition struct {
	image     *imageWriter
	partition *gpt.Partition
}

func (w *imageWriter) addPartition(gptType gpt.Type, name string, id string) (*gpt.Partition, error) {
	if w.table == nil {
		w.table = &gpt.Table{
			PhysicalSectorSize: blockSize,
			LogicalSectorSize:  blockSize,
		}
	}

	var start uint64 = 40

	if len(w.table.Partitions) > 0 {
		lastPart := w.table.Partitions[len(w.table.Partitions)-1]
		start = util.Align(lastPart.End, 8)
	} else {
		if err := w.closeFile(); err != nil {
			return nil, err
		} else if file, err := os.OpenFile(w.path, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644); err != nil {
			return nil, err
		} else {
			w.file = file
		}
	}

	partition := &gpt.Partition{
		Start: start,
		Type:  gptType,
		Name:  name,
		GUID:  id,
	}

	w.table.Partitions = append(w.table.Partitions, partition)

	return partition, nil
}

func (w *imageWriter) addSize(gptType gpt.Type, name string, id string, size uint64) (*partitionWriter, error) {
	partition, err := w.addPartition(gptType, name, id)

	if err != nil {
		return nil, err
	}

	size = util.Align(size, uint64(w.table.LogicalSectorSize))

	partition.Size = size
	partition.End = partition.Start + (size / uint64(w.table.LogicalSectorSize)) - 1

	if err := w.openFile(); err != nil {
		return nil, err
	}

	offset := int64(partition.Start) * int64(w.table.LogicalSectorSize)

	if _, err := w.file.WriteAt([]byte{0}, offset+int64(size)-1); err != nil {
		return nil, fmt.Errorf("error allocating partition: %v", err)
	}

	log.Printf("opening partition %d with offset %d and length %d", len(w.table.Partitions)-1, offset, size)
	return &partitionWriter{image: w, offset: offset, length: int64(size)}, nil
}

func (w *imageWriter) open(index int) (*partitionWriter, error) {
	if err := w.openFile(); err != nil {
		return nil, err
	}
	partition := w.table.Partitions[index]
	offset := int64(partition.Start) * int64(w.table.LogicalSectorSize)
	log.Printf("opening partition %d with offset %d and length %d", index, offset, partition.Size)
	return &partitionWriter{image: w, offset: offset, length: int64(partition.Size)}, nil
}

func (w *imageWriter) openExternal(gptType gpt.Type, name string, id string) (*externalPartition, error) {
	partition, err := w.addPartition(gptType, name, id)

	if err != nil {
		return nil, err
	}

	if err := w.closeFile(); err != nil {
		return nil, err
	}

	return &externalPartition{
		image:     w,
		partition: partition,
	}, nil
}

func (w *imageWriter) openFile() error {
	if w.file == nil {
		file, err := os.OpenFile(w.path, os.O_RDWR, 0644)

		if err != nil {
			return err
		}

		w.file = file

		size, err := file.Seek(0, io.SeekEnd)

		if err != nil {
			return err
		}

		log.Printf("reported disk size %d", size)
	}

	return nil
}

func (w *imageWriter) closeFile() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}

		w.file = nil
	}

	return nil
}

func (w *imageWriter) finish() error {
	if len(w.table.Partitions) == 0 {
		return errors.New("no partitions written")
	}

	size := int64(w.table.Partitions[len(w.table.Partitions)-1].End+40) * int64(w.table.LogicalSectorSize)

	size = util.Align(size, distAlignment)

	if err := w.openFile(); err != nil {
		return err
	}

	defer w.file.Close()

	log.Printf("disk size is %d (%d sectors)", size, int64(w.table.Partitions[len(w.table.Partitions)-1].End+40))

	return w.table.Write(w.file, size)
}

func (p *partitionWriter) Close() error {
	return nil
}

func (p *partitionWriter) Len() int64 {
	return p.length
}

func (p *partitionWriter) SectorSize() int {
	return p.image.table.LogicalSectorSize
}

func (p *partitionWriter) ReadAt(bs []byte, off int64) (n int, err error) {
	n, err = p.image.file.ReadAt(bs, p.offset+off)

	return n, err
}

func (p *partitionWriter) WriteAt(bs []byte, off int64) (n int, err error) {
	n, err = p.image.file.WriteAt(bs, p.offset+off)

	if err != nil {
		panic(fmt.Errorf("failed to write %d bytes at %d + %d", len(bs), p.offset, off))
	}

	return n, err
}

func (p *partitionWriter) Write(bs []byte) (int, error) {
	written, err := p.WriteAt(bs, p.pos)

	if err == nil {
		p.pos += int64(written)
	}

	return written, err
}

func (p *externalPartition) getOffset() int64 {
	return int64(p.partition.Start) * int64(p.image.table.LogicalSectorSize)
}

func (p *externalPartition) close() error {
	info, err := os.Stat(p.image.path)

	if err != nil {
		return fmt.Errorf("error inspecting %s: %v", p.image.path, err)
	}

	p.partition.End = util.CountBlocks(uint64(info.Size()), uint64(p.image.table.LogicalSectorSize)) - 1

	log.Printf("external partition ends at %d", info.Size())
	return nil
}
