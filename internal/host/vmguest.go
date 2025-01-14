package host

import (
	"fmt"
	"io"
	"net"
	gorpc "net/rpc"

	"github.com/Code-Hex/vz/v3"
	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/rpc"
	"github.com/amadigan/macoby/internal/util"
)

func (vm *VirtualMachine) Write(path string, data []byte) error {
	//nolint:wrapcheck
	return vm.client.Write(rpc.WriteRequest{Path: path, Data: data}, nil)
}

func (vm *VirtualMachine) Mkdir(path string) error {
	//nolint:wrapcheck
	return vm.client.Mkdir(path, nil)
}

func (vm *VirtualMachine) Mount(source, target, fstype string, flags []string) error {
	//nolint:wrapcheck
	return vm.client.Mount(rpc.MountRequest{FS: fstype, Device: source, Target: target, Flags: flags}, nil)
}

func (vm *VirtualMachine) Run(req rpc.Command) (rpc.CommandOutput, error) {
	var out rpc.CommandOutput
	err := vm.client.Run(req, &out)

	//nolint:wrapcheck
	return out, err
}

func (vm *VirtualMachine) Launch(req rpc.Command) (int64, error) {
	var pid int64
	err := vm.client.Launch(req, &pid)

	//nolint:wrapcheck
	return pid, err
}

func (vm *VirtualMachine) WaitService(name string) (int, error) {
	var exit int
	err := vm.client.Wait(name, &exit)

	//nolint:wrapcheck
	return exit, err
}

func (vm *VirtualMachine) Listen(network, address string) error {
	//nolint:wrapcheck
	return vm.client.Listen(rpc.ListenRequest{Network: network, Address: address}, nil)
}

func (vm *VirtualMachine) Signal(pid int64, sig int) error {
	//nolint:wrapcheck
	return vm.client.Signal(rpc.SignalRequest{Pid: pid, Signal: sig}, nil)
}

func (vm *VirtualMachine) Metrics(names []string) (rpc.Metrics, error) {
	var metrics rpc.Metrics
	err := vm.client.Metrics(names, &metrics)
	//nolint:wrapcheck
	return metrics, err
}

func (vm *VirtualMachine) DialUDP(network string, laddr *net.UDPAddr, raddr *net.UDPAddr) (net.PacketConn, error) {
	conn, err := vm.vsock.Connect(1)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to vsock: %w", err)
	}

	//nolint:wrapcheck
	return rpc.DialUDP(gorpc.NewClient(conn), network, laddr, raddr)
}

func (vm *VirtualMachine) ListenUDP(network string, laddr *net.UDPAddr) (net.PacketConn, error) {
	conn, err := vm.vsock.Connect(1)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to vsock: %w", err)
	}

	//nolint:wrapcheck
	return rpc.ListenUDP(gorpc.NewClient(conn), network, laddr)
}

func (vm *VirtualMachine) DialUnixgram(network string, laddr *net.UnixAddr, raddr *net.UnixAddr) (net.PacketConn, error) {
	conn, err := vm.vsock.Connect(1)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to vsock: %w", err)
	}

	//nolint:wrapcheck
	return rpc.DialUnix(gorpc.NewClient(conn), network, laddr, raddr)
}

func (vm *VirtualMachine) ListenUnixgram(network string, laddr *net.UnixAddr) (*rpc.DatagramClient, error) {
	conn, err := vm.vsock.Connect(1)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to vsock: %w", err)
	}

	//nolint:wrapcheck
	return rpc.ListenUnix(gorpc.NewClient(conn), network, laddr)
}

func (vm *VirtualMachine) Dial(network, address string) (net.Conn, error) {
	conn, err := vm.vsock.Connect(2)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to vsock: %w", err)
	}

	//nolint:wrapcheck
	return rpc.Dial(conn, network, address)
}

func (vm *VirtualMachine) Forward(listener net.Listener, network, address string) {
	vm.mutex.Lock()
	vm.listeners[listener] = struct{}{}
	vm.mutex.Unlock()

	applog.FanOut(listener.Accept, func(conn net.Conn) {
		defer conn.Close()

		remote, err := vm.Dial(network, address)
		if err != nil {
			log.Errorf("failed to dial %s: %v", address, err)

			return
		}

		defer remote.Close()

		go func() {
			_, _ = io.Copy(remote, conn)
		}()

		_, _ = io.Copy(conn, remote)
	}, log)

	vm.mutex.Lock()
	delete(vm.listeners, listener)
	vm.mutex.Unlock()
}

func (vm *VirtualMachine) Shutdown() error {
	shutdown := util.Await(func() (struct{}, error) {
		err := vm.client.Shutdown(struct{}{}, nil)

		//nolint:wrapcheck
		return struct{}{}, err
	})

	vm.mutex.Lock()
	for listener := range vm.listeners {
		_ = listener.Close()
	}

	vm.listeners = map[net.Listener]struct{}{}
	vm.mutex.Unlock()

	if _, err := shutdown(); err != nil {
		return err
	}

	if err := vm.rpcConn.Close(); err != nil {
		return fmt.Errorf("failed to close rpc connection: %w", err)
	}

	stateCh := vm.vm.StateChangedNotify()

	for state := range stateCh {
		log.Infof("vm shutting down... state: %s", state)

		if state == vz.VirtualMachineStateStopped {
			break
		} else if state == vz.VirtualMachineStateError {
			return fmt.Errorf("vm failed to shutdown, state: %s", state)
		}
	}

	return nil
}
