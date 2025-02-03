package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// VirtualMachineState... stores persistent information across VM restarts.
type DaemonState struct {
	Status        Status `json:"status,omitempty"`
	MACAddress    string `json:"mac-address,omitempty"`
	MachineID     []byte `json:"machine-id,omitempty"`
	ControlSocket string `json:"control-socket,omitempty"`
	IPv4Address   string `json:"ipv4-address,omitempty"`
}

type Status string

const (
	StatusStopped  Status = "stopped"
	StatusRunning  Status = "running"
	StatusStarting Status = "starting"
	StatusStopping Status = "stopping"
)

func OpenDaemonState(path string, status Status, ch <-chan DaemonState) (DaemonState, chan struct{}, error) {
	bs, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return DaemonState{}, nil, fmt.Errorf("failed to read daemon state file %s: %w", path, err)
	}

	var state DaemonState
	_ = json.Unmarshal(bs, &state)

	done := make(chan struct{})

	go func() {
		for update := range ch {
			if update.Status != "" {
				state.Status = update.Status
			}

			if update.MACAddress != "" {
				state.MACAddress = update.MACAddress
			}

			if len(update.MachineID) > 0 {
				state.MachineID = update.MachineID
			}

			if update.ControlSocket != "" {
				state.ControlSocket = update.ControlSocket
			}

			if update.IPv4Address != "" {
				state.IPv4Address = update.IPv4Address
			}

			if newbs, err := json.Marshal(state); err != nil {
				log.Warnf("failed to marshal daemon state: %v", err)
			} else if !bytes.Equal(bs, newbs) {
				//nolint:gosec
				if err := os.WriteFile(path, bs, 0644); err != nil {
					log.Warnf("failed to write daemon state: %v", err)
				} else {
					bs = newbs
				}
			}
		}

		close(done)
	}()

	return state, done, nil
}
