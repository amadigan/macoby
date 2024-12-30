package util

import "github.com/google/uuid"

// OSName ... The name of the operating system
const OSName string = "pillar"

var DockerDiskId uuid.UUID = uuid.MustParse("3d6084eb-bba5-45cc-8397-bd1bff8b8b98")
var DockerPartitionId uuid.UUID = uuid.MustParse("3c02acf9-7129-4505-a831-08029d8dce6a")
var DockerFilesystemId uuid.UUID = uuid.MustParse("91e660e3-54df-43da-9ee8-5252830f3572")
var DockerFilesystemLabel = "docker"

type CompressMode string

const (
	CompressNone  CompressMode = "none"
	CompressGZip  CompressMode = "gzip"
	CompressLZMA  CompressMode = "lzma"
	CompressLZO   CompressMode = "lzo"
	CompressLZ4   CompressMode = "lz4"
	CompressXZ    CompressMode = "xz"
	CompressBzip2 CompressMode = "bzip2"
)
