package config

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/pbnjay/memory"
)

type Layout struct {
	Ram           uint64                `json:"ram,omitempty"`
	Cpu           uint                  `json:"cpu,omitempty"`
	Kernel        *Path                 `json:"kernel,omitempty"`
	Root          *Path                 `json:"root,omitempty"`
	Disks         map[string]*DiskImage `json:"disks"`
	Shares        map[string]*Share     `json:"shares"`
	DockerSocket  DockerSocket          `json:"docker-socket"`
	ControlSocket *Path                 `json:"control,omitempty"`
	Sockets       map[string]string     `json:"sockets"`
	Console       bool                  `json:"-"`
	JsonConfigs   map[string]any        `json:"json-conf"`
	HostIface     string                `json:"host-iface,omitempty"`
	Sysctl        map[string]string     `json:"sysctl"`
	StateFile     *Path                 `json:"state-file,omitempty"`
	Log           LogConfig             `json:"logs"`
	DockerConfig  map[string]any        `json:"dockerd"`
}

type DiskImage struct {
	Mount         string   `json:"mount"`
	Size          string   `json:"size"`
	FS            string   `json:"fs"`
	FormatOptions []string `json:"mkfs"`
	ReadOnly      bool     `json:"ro,omitempty"`
	Options       []string `json:"opts"`
	Path          *Path    `json:"path,omitempty"`
}

type DockerSocket struct {
	HostPath      Paths  `json:"host"`
	ContainerPath string `json:"container"`
}

type Share struct {
	Source   *Path
	ReadOnly bool
}

type LogConfig struct {
	Level     string            `json:"level,omitempty"`
	Directory *Path             `json:"dir,omitempty"`
	Streams   map[string]string `json:"streams,omitempty"`
}

func (s *Share) UnmarshalText(data []byte) error {
	if str := string(data); strings.HasPrefix(str, "ro:") {
		s.ReadOnly = true
		s.Source = &Path{Original: str[3:]}
	} else {
		s.ReadOnly = false
		s.Source = &Path{Original: str}
	}

	return nil
}

func (l *Layout) SetDefaults() {
	if l.Ram == 0 {
		total := memory.TotalMemory()
		l.Ram = total / 4 / 1024 / 1024
	}

	if l.Cpu == 0 {
		// #nosec G115
		l.Cpu = uint(runtime.NumCPU())
	}

	if l.Kernel == nil || l.Kernel.Original == "" {
		l.Kernel = &Path{Original: fmt.Sprintf("${%s}/linux/kernel", HomeEnv)}
	}

	if l.Root == nil || l.Root.Original == "" {
		l.Root = &Path{Original: fmt.Sprintf("${%s}/linux/rootfs.img", HomeEnv)}
	}

	for label, disk := range l.Disks {
		if disk.FS == "" {
			disk.FS = "ext4"
		}

		if disk.Path == nil || disk.Path.Original == "" {
			disk.Path = &Path{Original: fmt.Sprintf("${%s}/data/%s.img", HomeEnv, label)}
		}
	}

	if l.DockerSocket.ContainerPath == "" {
		l.DockerSocket.ContainerPath = "/run/docker.sock"
	}

	if l.StateFile == nil || l.StateFile.Original == "" {
		l.StateFile = &Path{Original: fmt.Sprintf("${%s}/data/state.json", HomeEnv)}
	}

	if l.Log.Directory == nil || l.Log.Directory.Original == "" {
		l.Log.Directory = &Path{Original: fmt.Sprintf("${HOME}/Library/Logs/%s", AppID)}
	}
}

func (l *Layout) SetDefaultSockets() {
	if len(l.DockerSocket.HostPath) == 0 {
		l.DockerSocket.HostPath = Paths{{Original: fmt.Sprintf("${%s}/run/docker.sock", HomeEnv)}}
	}

	if l.ControlSocket == nil || l.ControlSocket.Original == "" {
		l.ControlSocket = &Path{Original: fmt.Sprintf("${%s}/run/%s.sock", HomeEnv, Name)}
	}
}

var layoutValidator = newFieldValidator(Layout{})
var diskImageValidator = newFieldValidator(DiskImage{})
var dockerSocketValidator = newFieldValidator(DockerSocket{})
var logConfigValidator = newFieldValidator(LogConfig{})

func (l *Layout) UnmarshalJSON(data []byte) error {
	if err := layoutValidator.Validate(data); err != nil {
		return err
	}

	type layout Layout
	return json.Unmarshal(data, (*layout)(l))
}

func (d *DiskImage) UnmarshalJSON(data []byte) error {
	if err := diskImageValidator.Validate(data); err != nil {
		return err
	}

	type diskImage DiskImage
	return json.Unmarshal(data, (*diskImage)(d))
}

func (d *DockerSocket) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		d.ContainerPath = str

		return nil
	}

	if err := dockerSocketValidator.Validate(data); err != nil {
		return err
	}

	type dockerSocket DockerSocket
	return json.Unmarshal(data, (*dockerSocket)(d))
}

func (d DockerSocket) MarshalJSON() ([]byte, error) {
	if len(d.HostPath) == 0 {
		return json.Marshal(d.ContainerPath)
	}

	return json.Marshal(map[string]any{
		"host":      d.HostPath,
		"container": d.ContainerPath,
	})
}

func (l *LogConfig) UnmarshalJSON(data []byte) error {
	var str string

	if err := json.Unmarshal(data, &str); err == nil {
		l.Directory = &Path{Original: str}

		return nil
	}

	if err := logConfigValidator.Validate(data); err != nil {
		return err
	}

	type logConfig LogConfig
	return json.Unmarshal(data, (*logConfig)(l))
}

func (l LogConfig) MarshalJSON() ([]byte, error) {
	if l.Level == "" && len(l.Streams) == 0 {
		return json.Marshal(l.Directory)
	}

	m := map[string]any{"dir": l.Directory}

	if l.Level != "" {
		m["level"] = l.Level
	}

	if len(l.Streams) > 0 {
		m["streams"] = l.Streams
	}

	return json.Marshal(m)
}