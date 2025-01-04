package host

import "strings"

type Layout struct {
	Ram         uint64               `json:"ram,omitempty"`
	Cpu         uint64               `json:"cpu,omitempty"`
	Disks       map[string]DiskImage `json:"disks"`
	Shares      []Share              `json:"shares"`
	Sockets     map[string]string    `json:"sockets"`
	Console     bool                 `json:"-"`
	JsonConfigs map[string]any       `json:"json-conf"`
	HostIface   string               `json:"host-iface,omitempty"`
	Sysctl      map[string]string    `json:"sysctl"`
}

type DiskImage struct {
	Size          uint64   `json:"size"`
	FS            string   `json:"fs"`
	FormatOptions []string `json:"formatOptions"`
	ReadOnly      bool     `json:"ro"`
	Options       string   `json:"opts"`
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
	if str := string(data); strings.HasPrefix(str, "ro:") {
		s.ReadOnly = true
		s.Source = str[3:]
	} else {
		s.ReadOnly = false
		s.Source = str
	}

	return nil
}
