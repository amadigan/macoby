package main

import (
	"fmt"
	"os"

	"github.com/amadigan/macoby/internal/image"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/google/uuid"
)

func main() {

	if len(os.Args) != 3 {
		fmt.Println("Usage: buildroot infile outfile")
		os.Exit(1)
	}

	diskId := uuid.New().String()
	rootId := uuid.New().String()

	err := image.WrapImage(os.Args[1], os.Args[2], image.WrapOptions{
		DiskID:      diskId,
		PartitionID: rootId,
		Type:        gpt.LinuxRootArm64,
		Name:        "root",
	})

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}
