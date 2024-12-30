package image

import (
	"io"
	"os"

	"github.com/diskfs/go-diskfs/partition/gpt"
)

type WrapOptions struct {
	// The type of the file system
	Type gpt.Type
	// The name of the partition
	Name string
	// The GUID of the Disk
	DiskID string
	// The GUID of the Partition
	PartitionID string
}

// WrapImage... Wrap a single filesystem image into a single partition GPT disk image
func WrapImage(imagePath string, outpath string, options WrapOptions) error {
	stat, err := os.Stat(imagePath)

	if err != nil {
		return err
	}

	w := imageWriter{
		path: outpath,
		table: &gpt.Table{
			LogicalSectorSize:  blockSize,
			PhysicalSectorSize: blockSize,
			GUID:               options.DiskID,
		},
	}

	partition, err := w.addSize(options.Type, options.Name, options.PartitionID, uint64(stat.Size()))
	defer w.finish()

	if err != nil {
		return err
	}

	image, err := os.Open(imagePath)

	if err != nil {
		return err
	}

	defer image.Close()

	if _, err := io.Copy(partition, image); err != nil {
		return err
	}

	return nil
}
