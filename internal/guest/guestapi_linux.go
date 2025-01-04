package guest

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/amadigan/macoby/internal/block"
	"github.com/amadigan/macoby/internal/vzapi"
	"github.com/mdlayher/vsock"
	"golang.org/x/sys/unix"
)

const ProtocolVersion = 1

type Guest struct {
	processeses map[int]*os.Process
	listener    *vsock.Listener

	mutex sync.Mutex
}

var _ vzapi.Handler = &Guest{}

func (g *Guest) Start(ctx context.Context) error {
	listener, err := vsock.ListenContextID(3, 1, nil)

	if err != nil {
		return err
	}

	g.mutex.Lock()
	g.listener = listener
	g.mutex.Unlock()

	for {
		log.Printf("Waiting for vsock API connection")
		conn, err := listener.Accept()

		log.Printf("Accepted vsock API connection")

		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break
			}

			return err
		}

		go vzapi.Handle(ctx, conn, g)
	}

	return nil
}

func (g *Guest) Stop(ctx context.Context) error {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	if g.listener == nil {
		return nil
	}

	if err := g.listener.Close(); err != nil {
		return err
	}

	g.listener = nil

	return nil
}

func (g *Guest) Info(_ context.Context, _ vzapi.InfoRequest) vzapi.InfoResponse {
	return vzapi.InfoResponse{
		ProtocolVersion: ProtocolVersion,
	}
}

func stopAllProcesses(ctx context.Context, procs map[int]*os.Process) {
	for _, p := range procs {
		_ = p.Signal(syscall.SIGTERM)
	}

	done := make(chan struct{})

	go func() {
		for _, proc := range procs {
			_, _ = proc.Wait()
		}

		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		for _, p := range procs {
			_ = p.Kill()
		}
	}
}

func (g *Guest) Shutdown(ctx context.Context) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)

	stopAllProcesses(sctx, g.processeses)

	cancel()

	g.processeses = nil

	if err := UnmountAll(ctx); err != nil {
		panic(err)
	}

	if err := syscall.Reboot(syscall.LINUX_REBOOT_CMD_POWER_OFF); err != nil {
		panic(err)
	}
}

func (g *Guest) Mount(ctx context.Context, req vzapi.MountRequest) error {
	var flags uintptr

	device := req.Device

	if req.ReadOnly {
		flags |= unix.MS_RDONLY
	} else if req.FS == "btrfs" || req.FS == "ext4" || req.FS == "xfs" || req.FS == "swap" {
		table := block.PartitionTable{Device: device}

		if err := table.ReadDevice(); err != nil {
			return fmt.Errorf("Unable to read device %s: %v", req.Device, err)
		}

		if table.Type != block.TableRaw {
			// scan for a partition with the correct filesystem
			found := false

			for _, part := range table.Partitions {
				if string(part.FilesystemType) == req.FS {
					device = part.Device
					found = true

					break
				}
			}

			if !found {
				return fmt.Errorf("No partition found with filesystem %s", req.FS)
			}
		} else if part := table.Partitions[0]; part.FilesystemType == block.FSnone {
			switch req.FS {
			case "ext4":
				if err := mkext4(device, req.FormatOptions); err != nil {
					return err
				}
			case "btrfs":
				if err := mkbtrfs(device, req.FormatOptions); err != nil {
					return err
				}
			case "swap":
				if err := mkswap(device, req.FormatOptions); err != nil {
					return err
				}
			default:
				return fmt.Errorf("Unsupported mkfs filesystem %s", req.FS)
			}
		}
	}

	if err := os.MkdirAll(req.Target, 0755); err != nil {
		return err
	}

	log.Printf("Mounting %s on %s with flags %x", device, req.Target, flags)

	if err := unix.Mount(device, req.Target, req.FS, flags, req.Options); err != nil {
		return err
	}

	return nil
}

func (g *Guest) Write(ctx context.Context, req vzapi.WriteRequest) error {
	if req.Directory {
		if err := os.MkdirAll(req.Path, 0755); err != nil {
			return err
		}

		return nil
	}

	data := req.Data

	if req.Base64 {
		data = make([]byte, 0, len(req.Data))

		if _, err := base64.StdEncoding.Decode(data, req.Data); err != nil {
			return err
		}
	}

	// get directory of path
	dir := req.Path

	if idx := strings.LastIndexByte(req.Path, '/'); idx != -1 {
		dir = req.Path[:idx]
	}

	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	if err := os.WriteFile(req.Path, data, 0644); err != nil {
		return err
	}

	return nil
}

func (g *Guest) Connect(ctx context.Context, req vzapi.ConnectRequest) (net.Conn, error) {
	log.Printf("Connecting to %s://%s", req.Network, req.Address)
	return net.Dial(req.Network, req.Address)
}

func (g *Guest) Launch(ctx context.Context, req vzapi.LaunchRequest) error {
	cmd := exec.Command(req.Path)

	cmd.Args = req.Args
	cmd.Env = req.Env

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	g.mutex.Lock()
	defer g.mutex.Unlock()

	if g.processeses == nil {
		g.processeses = make(map[int]*os.Process)
	}

	g.processeses[cmd.Process.Pid] = cmd.Process

	return nil
}
