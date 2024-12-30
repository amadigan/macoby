package conf

type MountpointConf struct {
	Device string `json:"device"`
	FSType string `json:"fstype"`
	Format bool   `json:"format"`
}

type VMConf struct {
	Mountpoints map[string]MountpointConf `json:"mountpoints"`
	Service     string                    `json:"service"`
}
