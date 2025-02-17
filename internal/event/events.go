package event

func init() {
	RegisterEventType(OpenLogFile{})
	RegisterEventType(DeleteLogFile{})
	RegisterEventType(Metrics{})
	RegisterEventType(Status(""))
}

type OpenLogFile struct {
	Path   string
	Stream string
}

type DeleteLogFile struct {
	Path   string
	Stream string
}

type Metrics struct {
	Uptime   int64
	Loads    [3]uint64
	Mem      uint64
	MemFree  uint64
	Swap     uint64
	SwapFree uint64
	Procs    uint16
	Disks    map[string]DiskMetrics
}

type DiskMetrics struct {
	Total     uint64
	Free      uint64
	MaxFiles  uint64
	FreeFiles uint64
}

type LogFile struct {
	Path   string
	Offset int64
}

type Status string

const (
	StatusBooting   Status = "booting"
	StatusReady     Status = "ready"
	StatusStopping  Status = "stopping"
	StatusModifying Status = "modifying"
	StatusStopped   Status = "stopped"
)

type Sync struct {
	Status  Status               `json:"status"`
	Metrics Metrics              `json:"metrics"`
	Logs    map[string][]LogFile `json:"logs"`
}
