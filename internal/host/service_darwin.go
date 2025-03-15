package host

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/amadigan/macoby/internal/rpc"
)

const DefaultPATH = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

type SystemdNotify struct {
	Data map[string]string
	Addr net.UnixAddr
}

func (n SystemdNotify) IsReady() bool {
	return n.Data["READY"] == "1"
}

func ReadSDNotify(client *rpc.DatagramClient) (*SystemdNotify, error) {
	bs, _, err := client.Read(8192)
	if err != nil {
		return nil, fmt.Errorf("failed to read from notify socket: %w", err)
	}

	rv := &SystemdNotify{Data: map[string]string{}}

	for line := range strings.SplitSeq(string(bs), "\n") {
		parts := strings.SplitN(line, "=", 2)

		if len(parts) != 2 {
			continue
		}

		rv.Data[parts[0]] = parts[1]
	}

	return rv, nil
}

type Service struct {
	name string
	pid  int64
	ch   <-chan int
	vm   *VirtualMachine
}

func (vm *VirtualMachine) LaunchService(ctx context.Context, cmd rpc.Command) (Service, error) {
	svc := Service{vm: vm, name: cmd.Name}
	if cmd.Name == "" {
		return svc, fmt.Errorf("missing service name for launch of %s", cmd.Path)
	}

	sockPath := fmt.Sprintf("/run/%s.sock", cmd.Name)

	conn, err := vm.ListenUnixgram("unixgram", &net.UnixAddr{Name: sockPath, Net: "unixgram"})
	if err != nil {
		return svc, fmt.Errorf("failed to listen on vm:%s: %w", sockPath, err)
	}

	defer conn.Close()

	if len(cmd.Env) == 0 {
		cmd.Env = make([]string, 1, 2)
		cmd.Env[0] = "PATH=" + DefaultPATH
	}

	cmd.Env = append(cmd.Env, "NOTIFY_SOCKET="+sockPath)

	pid, err := vm.Launch(cmd)
	if err != nil {
		return svc, fmt.Errorf("failed to launch %s: %w", cmd.Name, err)
	}

	svc.pid = pid
	rc := make(chan int)
	svc.ch = rc

	go waitNotify(conn, rc)
	go vm.waitService(cmd.Name, rc)

	select {
	case <-ctx.Done():
		return svc, fmt.Errorf("launch of %s timed out", cmd.Name)
	case code := <-rc:
		switch code {
		case 0:
			return svc, nil
		case -1:
			return svc, fmt.Errorf("service %s exited", cmd.Name)
		default:
			return svc, fmt.Errorf("service %s exited with code %d", cmd.Name, code)
		}
	}
}

func (vm *VirtualMachine) waitService(name string, ch chan int) {
	defer close(ch)
	exit, err := vm.WaitService(name)

	switch {
	case err != nil:
		ch <- 1
	case exit == 0:
		ch <- -1
	default:
		ch <- exit
	}
}

func (svc *Service) Wait() int {
	return <-svc.ch
}

func (svc *Service) Signal(sig int) error {
	return svc.vm.Signal(svc.pid, sig)
}

func waitNotify(conn *rpc.DatagramClient, ch chan int) {
	for {
		if notify, err := ReadSDNotify(conn); err != nil {
			log.Errorf("failed to read from notify socket: %v", err)

			defer func() {
				_ = recover()
			}()

			ch <- -1

			return
		} else if notify.IsReady() {
			ch <- 0

			return
		}
	}
}
