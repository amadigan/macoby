package guest

import (
	"context"
	"fmt"
	"io"
	golog "log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/rpc"
	"github.com/mdlayher/vsock"
	"golang.org/x/sys/unix"
)

var _ rpc.Guest = &Guest{}

func StartGuest() error {
	bufsize := 32

	if envsize := os.Getenv("EVENT_BUFFER_SIZE"); envsize != "" {
		sz, err := strconv.Atoi(envsize)

		if err != nil {
			log.Warnf("Invalid EVENT_BUFFER_SIZE %s: %v", envsize, err)
		}

		bufsize = sz
	}

	// start the proxy server on port 2
	proxyListener, err := vsock.ListenContextID(3, 2, nil)

	if err != nil {
		return err
	}

	go applog.FanOut(proxyListener.Accept, rpc.ServeStreamProxy, log)

	// bind port 1 for the guest API
	apiListener, err := vsock.ListenContextID(3, 1, nil)

	if err != nil {
		return err
	}

	defer apiListener.Close()

	// start the event emitter, this notifies the host the guest has started
	eventConn, err := vsock.Dial(2, 1, nil)

	if err != nil {
		return err
	}

	emitter := rpc.NewEmitter(eventConn, bufsize)

	// send logs to the event emitter
	golog.SetOutput(io.MultiWriter(rpc.NewEmitterWriter(emitter, "guest", rpc.LogInternal), os.Stderr))

	g := &Guest{emitter: emitter, processeses: map[int]*os.Process{}}

	log.Info("guest started")

	// wait for the host to connect
	conn, err := apiListener.Accept()

	if err != nil {
		return err
	}

	defer conn.Close()

	// subsequent requests go to the DatagramProxy
	go applog.FanOut(apiListener.Accept, rpc.ServeDatagramProxy, log)

	// handle the host RPC calls
	rpc.ServeGuestAPI(g, conn)

	return nil
}

func (g *Guest) Init(req rpc.InitRequest, out *rpc.InitResponse) error {
	if err := OverlayRoot(req.OverlaySize); err != nil {
		return err
	}

	if err := MountProc(); err != nil {
		return err
	}

	if err := MountSys(); err != nil {
		return err
	}

	if err := MountCgroup(); err != nil {
		return err
	}

	go StartClockSync(10*time.Second, make(chan struct{}))

	if ipv4, ipv6, err := InitializeNetwork(); err == nil {
		*out = rpc.InitResponse{IPv4: ipv4, IPv6: ipv6}
	} else {
		return err
	}

	if err := sysctl(req.Sysctl); err != nil {
		return err
	}

	return nil
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

func (g *Guest) Shutdown(struct{}, *struct{}) error {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	ctx := context.Background()

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

	return nil
}

func (g *Guest) Mount(req rpc.MountRequest, _ *struct{}) error {
	var fsOpts []string = make([]string, 0, len(req.Flags))
	var flags uintptr

	for _, opt := range req.Flags {
		switch opt {
		case "ro":
			flags |= unix.MS_RDONLY
		case "rw":
			flags &^= unix.MS_RDONLY
		case "noatime":
			flags |= unix.MS_NOATIME
		case "nodev":
			flags |= unix.MS_NODEV
		case "nosuid":
			flags |= unix.MS_NOSUID
		case "bind":
			flags |= unix.MS_BIND
		case "remount":
			flags |= unix.MS_REMOUNT
		case "recursive":
			flags |= unix.MS_REC
		case "shared":
			flags |= unix.MS_SHARED
		case "slave":
			flags |= unix.MS_SLAVE
		case "private":
			flags |= unix.MS_PRIVATE
		case "unbindable":
			flags |= unix.MS_UNBINDABLE
		case "move":
			flags |= unix.MS_MOVE
		case "dirsync":
			flags |= unix.MS_DIRSYNC
		case "noexec":
			flags |= unix.MS_NOEXEC
		case "synchronous":
			flags |= unix.MS_SYNCHRONOUS
		case "lazytime":
			flags |= unix.MS_LAZYTIME
		case "mand":
			flags |= unix.MS_MANDLOCK
		case "relatime":
			flags |= unix.MS_RELATIME
		case "strictatime":
			flags |= unix.MS_STRICTATIME
		case "silent":
			flags |= unix.MS_SILENT
		default:
			fsOpts = append(fsOpts, opt)
		}
	}

	if err := os.MkdirAll(req.Target, 0755); err != nil {
		return err
	}

	log.Infof("Mounting %s on %s with flags %x", req.Device, req.Target, flags)

	if err := unix.Mount(req.Device, req.Target, req.FS, flags, strings.Join(fsOpts, ",")); err != nil {
		return err
	}

	return nil
}

func sysctl(req map[string]string) error {
	for key, val := range req {
		chars := []byte(key)

		hasDot := false

		for i, c := range chars {
			if c == '.' {
				hasDot = true
				chars[i] = '/'
			} else if c == '/' {
				if hasDot {
					chars[i] = '.'
				} else {
					break
				}
			}
		}

		path := filepath.Clean("/proc/sys/" + string(chars))

		if !strings.HasPrefix(path, "/proc/sys/") {
			return fmt.Errorf("Invalid sysctl key: %s", key)
		}

		if err := os.WriteFile(path, []byte(val), 0644); err != nil {
			return fmt.Errorf("Failed to write %s: %v", key, err)
		}
	}

	return nil
}

func (g *Guest) Metrics(req []string, out *rpc.Metrics) error {

	*out = rpc.Metrics{}

	return nil
}
