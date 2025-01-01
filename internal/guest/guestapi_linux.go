package guest

import (
	"context"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/amadigan/macoby/internal/vzapi"
	"golang.org/x/sys/unix"
)

const ProtocolVersion = 1

type Guest struct {
	processeses map[int]*os.Process

	mutex sync.Mutex
}

var _ vzapi.Handler = &Guest{}

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

	if req.ReadOnly {
		flags |= unix.MS_RDONLY
	}

	if err := unix.Mount(req.Device, req.Target, req.FS, flags, ""); err != nil {
		return err
	}

	return nil
}


