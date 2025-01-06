package host

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/pbnjay/memory"
)

type Layout struct {
	Ram           uint64                `json:"ram,omitempty"`
	Cpu           int                   `json:"cpu,omitempty"`
	Kernel        string                `json:"kernel,omitempty"`
	Root          string                `json:"root,omitempty"`
	Disks         map[string]*DiskImage `json:"disks"`
	Shares        map[string]*Share     `json:"shares"`
	DockerSocket  DockerSocket          `json:"docker-socket"`
	ControlSocket string                `json:"control"`
	Sockets       map[string]string     `json:"sockets"`
	Console       bool                  `json:"-"`
	JsonConfigs   map[string]any        `json:"json-conf"`
	HostIface     string                `json:"host-iface,omitempty"`
	Sysctl        map[string]string     `json:"sysctl"`
	StateFile     string                `json:"state-file"`
}

type DiskImage struct {
	Mount         string   `json:"mount"`
	Size          string   `json:"size"`
	FS            string   `json:"fs"`
	FormatOptions []string `json:"mkfs"`
	ReadOnly      bool     `json:"ro"`
	Options       []string `json:"opts"`
	Path          string   `json:"path"`
}

type DockerSocket struct {
	HostPath      string `json:"host"`
	ContainerPath string `json:"container"`
}

type Share struct {
	Source   string
	ReadOnly bool
}

func (s Share) MarshalText() ([]byte, error) {
	if s.ReadOnly {
		return []byte("ro:" + s.Source), nil
	}

	return []byte(s.Source), nil
}

func (s *Share) UnmarshalText(data []byte) error {
	log.Infof("UnmarshalText: %s", data)

	if str := string(data); strings.HasPrefix(str, "ro:") {
		s.ReadOnly = true
		s.Source = str[3:]
	} else {
		s.ReadOnly = false
		s.Source = str
	}

	return nil
}

func (l *Layout) SetDefaults() {
	if l.Ram == 0 {
		total := memory.TotalMemory()
		l.Ram = total / 4 / 1024 / 1024
	}

	if l.Cpu == 0 {
		l.Cpu = runtime.NumCPU()
	}

	if l.Kernel == "" {
		l.Kernel = "linux/kernel-" + runtime.GOARCH
	}

	if l.Root == "" {
		l.Root = fmt.Sprintf("linux/rootfs-%s.sqsh", runtime.GOARCH)
	}

	for _, disk := range l.Disks {
		if disk.FS == "" {
			disk.FS = "ext4"
		}
	}

	if l.DockerSocket.ContainerPath == "" {
		l.DockerSocket.ContainerPath = "/run/docker.sock"
	}

	if l.ControlSocket == "" {
		l.ControlSocket = fmt.Sprintf("run/%s.sock", Name)
	}

	if l.StateFile == "" {
		l.StateFile = fmt.Sprintf("run/%s.state", Name)
	}
}

func ParseSize(size string) (uint64, error) {
	size = strings.ToUpper(size)

	var unit uint64 = 1

	switch {
	case strings.HasSuffix(size, "T"):
		unit = unit * 1024
		fallthrough
	case strings.HasSuffix(size, "G"):
		unit = unit * 1024
		fallthrough
	case strings.HasSuffix(size, "M"):
		unit = unit * 1024
		fallthrough
	case strings.HasSuffix(size, "K"):
		unit = unit * 1024
		size = size[:len(size)-1]
	}

	count, err := strconv.ParseUint(size, 10, 64)

	if err != nil {
		return 0, fmt.Errorf("failed to parse size %s: %w", size, err)
	}

	return count * unit, nil
}
