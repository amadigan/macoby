package guest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/event"
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
	applog.SetOutput(rpc.NewEmitterWriter(emitter, "guest", rpc.LogInternal))

	g := &Guest{emitter: emitter, processeses: map[string]*os.Process{}}

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

var log = applog.New("guest")

type Guest struct {
	processeses map[string]*os.Process
	emitter     chan<- rpc.LogEvent
	clockStop   chan struct{}

	mutex sync.Mutex
}

func (g *Guest) Write(req rpc.WriteRequest, _ *struct{}) error {
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

	if err := os.WriteFile(req.Path, req.Data, 0644); err != nil {
		return err
	}

	return nil
}

func (g *Guest) Mkdir(path string, _ *struct{}) error {
	return os.MkdirAll(path, 0755)
}

func (g *Guest) Run(req rpc.Command, out *rpc.CommandOutput) error {
	cmd := exec.Command(req.Path)
	cmd.Args = req.Args
	cmd.Env = req.Env
	cmd.Dir = req.Dir
	cmd.Stdin = bytes.NewReader(req.Input)

	outbs, err := cmd.CombinedOutput()

	*out = rpc.CommandOutput{Output: outbs}

	if err != nil {
		var exitErr *exec.ExitError

		if errors.As(err, &exitErr) {
			out.Exit = exitErr.ExitCode()
		} else {
			return err
		}
	}

	return nil
}

func (g *Guest) Launch(req rpc.Command, pid *int64) error {
	cmd := exec.Command(req.Path)
	cmd.Args = req.Args
	cmd.Env = req.Env
	cmd.Dir = req.Dir
	cmd.Stdin = bytes.NewReader(req.Input)

	name := req.Name

	if name == "" {
		name = req.Path
	}

	cmd.Stdout = rpc.NewEmitterWriter(g.emitter, name, rpc.LogStdout)
	cmd.Stderr = rpc.NewEmitterWriter(g.emitter, name, rpc.LogStderr)

	if err := cmd.Start(); err != nil {
		return err
	}

	*pid = int64(cmd.Process.Pid)

	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.processeses[name] = cmd.Process

	return nil
}

func (g *Guest) Wait(service string, exit *int) error {
	g.mutex.Lock()
	process := g.processeses[service]
	g.mutex.Unlock()

	if process == nil {
		return fmt.Errorf("no such process: %s", service)
	}

	state, err := process.Wait()

	if err != nil {
		return err
	}

	*exit = state.ExitCode()

	g.mutex.Lock()
	delete(g.processeses, service)
	g.mutex.Unlock()

	return nil
}

func (g *Guest) Release(service string, _ *struct{}) error {
	g.mutex.Lock()
	process := g.processeses[service]
	delete(g.processeses, service)
	g.mutex.Unlock()

	if process != nil {
		_ = process.Release()
	}

	return nil
}

func (g *Guest) Listen(req rpc.ListenRequest, _ *struct{}) error {
	return fmt.Errorf("not implemented")
}

func (g *Guest) Signal(req rpc.SignalRequest, _ *struct{}) error {
	if req.Service != "" {
		g.mutex.Lock()
		process := g.processeses[req.Service]
		g.mutex.Unlock()

		if process == nil {
			return fmt.Errorf("no such process: %s", req.Service)
		}

		return process.Signal(syscall.Signal(req.Signal))
	}

	return syscall.Kill(int(req.Pid), syscall.Signal(req.Signal))
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

	if err := unix.Mount("binfmt_misc", "/proc/sys/fs/binfmt_misc", "binfmt_misc", 0, ""); err != nil {
		return err
	}

	g.clockStop = make(chan struct{})

	go StartClockSync(10*time.Second, g.clockStop)

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

func stopAllProcesses(ctx context.Context, procs map[string]*os.Process) {
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
	close(g.clockStop)
	g.mutex.Lock()
	defer g.mutex.Unlock()

	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	stopAllProcesses(sctx, g.processeses)

	cancel()

	g.processeses = nil

	if err := UnmountAll(); err != nil {
		return err
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

func (g *Guest) Metrics(req []string, out *event.Metrics) error {
	rv := event.Metrics{
		Disks: make(map[string]event.DiskMetrics, len(req)),
	}

	for _, disk := range req {
		var stat syscall.Statfs_t

		if err := syscall.Statfs(disk, &stat); err != nil {
			return fmt.Errorf("Failed to statfs %s: %v", disk, err)
		}

		rv.Disks[disk] = event.DiskMetrics{
			Total:     stat.Blocks * uint64(stat.Bsize),
			Free:      stat.Bfree * uint64(stat.Bsize),
			MaxFiles:  stat.Files,
			FreeFiles: stat.Ffree,
		}
	}

	bs, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return fmt.Errorf("Failed to read /proc/meminfo: %v", err)
	}

	for _, line := range strings.Split(string(bs), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			if rv.Mem, err = parseMem(line); err != nil {
				return fmt.Errorf("Failed to parse MemTotal: %v", err)
			}
		} else if strings.HasPrefix(line, "MemFree:") {
			if rv.MemFree, err = parseMem(line); err != nil {
				return fmt.Errorf("Failed to parse MemFree: %v", err)
			}
		}
	}

	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return fmt.Errorf("Failed to get sysinfo: %v", err)
	}

	rv.Uptime = int64(info.Uptime)
	rv.Loads = [3]uint64{info.Loads[0], info.Loads[1], info.Loads[2]}
	rv.Swap = info.Totalswap
	rv.SwapFree = info.Freeswap
	rv.Procs = info.Procs

	*out = rv

	return nil
}

func (g *Guest) GC(struct{}, *struct{}) error {
	runtime.GC()

	return nil
}

func parseMem(line string) (val uint64, err error) {
	fields := strings.Fields(line)

	if len(fields) < 2 {
		return 0, fmt.Errorf("Invalid line: %s", line)
	}

	if val, err = strconv.ParseUint(fields[1], 10, 64); err != nil {
		return 0, fmt.Errorf("Failed to parse %s: %v", fields[1], err)
	}

	if len(fields) > 2 {
		switch fields[2] {
		case "kB":
			val *= 1024
		case "mB":
			val *= 1024 * 1024
		case "gB":
			val *= 1024 * 1024 * 1024
		case "tB":
			val *= 1024 * 1024 * 1024 * 1024
		}
	}

	return val, nil
}
