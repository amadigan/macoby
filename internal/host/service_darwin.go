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

	for _, line := range strings.Split(string(bs), "\n") {
		parts := strings.SplitN(line, "=", 2)

		if len(parts) != 2 {
			continue
		}

		rv.Data[parts[0]] = parts[1]
	}

	return rv, nil
}

func (vm *VirtualMachine) LaunchService(ctx context.Context, cmd rpc.Command) error {
	if cmd.Name == "" {
		return fmt.Errorf("missing service name for launch of %s", cmd.Path)
	}

	sockPath := fmt.Sprintf("/run/%s.sock", cmd.Name)

	conn, err := vm.ListenUnixgram("unixgram", &net.UnixAddr{Name: sockPath, Net: "unixgram"})
	if err != nil {
		return fmt.Errorf("failed to listen on vm:%s: %w", sockPath, err)
	}

	defer conn.Close()

	if len(cmd.Env) == 0 {
		cmd.Env = make([]string, 1, 2)
		cmd.Env[0] = "PATH=" + DefaultPATH
	}

	cmd.Env = append(cmd.Env, "NOTIFY_SOCKET="+sockPath)

	if _, err := vm.Launch(cmd); err != nil {
		return fmt.Errorf("failed to launch %s: %w", cmd.Name, err)
	}

	rc := make(chan int)
	defer close(rc)

	go waitNotify(conn, rc)

	go vm.waitService(cmd.Name, rc)

	select {
	case <-ctx.Done():
		return fmt.Errorf("launch of %s timed out", cmd.Name)
	case code := <-rc:
		switch code {
		case 0:
			return nil
		case -1:
			return fmt.Errorf("service %s exited", cmd.Name)
		default:
			return fmt.Errorf("service %s exited with code %d", cmd.Name, code)
		}
	}
}

func (vm *VirtualMachine) waitService(name string, ch chan int) {
	exit, err := vm.WaitService(name)

	defer func() {
		_ = recover()
	}()

	switch {
	case err != nil:
		ch <- 1
	case exit == 0:
		ch <- -1
	default:
		ch <- exit
	}
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
