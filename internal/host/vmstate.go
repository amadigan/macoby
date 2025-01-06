package host

// VirtualMachineState... stores persistent information across VM restarts.
type VirtualMachineState struct {
	MACAddress string `json:"mac-address"`
}
