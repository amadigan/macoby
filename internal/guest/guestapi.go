package guest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/rpc"
)

var log = applog.New("guest")

type Guest struct {
	processeses map[int]*os.Process
	emitter     chan<- rpc.LogEvent

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

	cmd.Stdout = rpc.NewEmitterWriter(g.emitter, req.Path, rpc.LogStdout)
	cmd.Stderr = rpc.NewEmitterWriter(g.emitter, req.Path, rpc.LogStderr)

	if err := cmd.Start(); err != nil {
		return err
	}

	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.processeses[cmd.Process.Pid] = cmd.Process

	*pid = int64(cmd.Process.Pid)

	return nil
}

func (g *Guest) Listen(req rpc.ListenRequest, _ *struct{}) error {
	return fmt.Errorf("not implemented")
}

func (g *Guest) Signal(req rpc.SignalRequest, _ *struct{}) error {
	return syscall.Kill(int(req.Pid), syscall.Signal(req.Signal))
}
