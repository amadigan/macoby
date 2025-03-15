package host

import (
	"context"
	"fmt"
	"io"
	"net"
	gorpc "net/rpc"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/event"
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

func (vm *VirtualMachine) ForwardStopLatch(listener net.Listener, network, address string, latch *StopLatch) {
	vm.mutex.Lock()
	vm.listeners[listener] = struct{}{}
	vm.mutex.Unlock()

	applog.FanOut(listener.Accept, func(conn net.Conn) {
		defer conn.Close()
		if latch != nil {
			latch.Add(1)
			defer latch.Add(-1)
		}

		remote, err := vm.Dial(network, address)
		if err != nil {
			log.Errorf("failed to dial %s: %v", address, err)

			return
		}

		go func() {
			defer remote.Close()
			_, _ = io.Copy(remote, conn)
			log.Debugf("remote disconnected %s from %s", remote.RemoteAddr(), conn.RemoteAddr())
		}()

		log.Debugf("forwarding %s to %s", conn.RemoteAddr(), remote.RemoteAddr())
		_, _ = io.Copy(conn, remote)
		log.Debugf("disconnected %s from %s", conn.RemoteAddr(), remote.RemoteAddr())
	}, log)

	vm.mutex.Lock()
	delete(vm.listeners, listener)
	vm.mutex.Unlock()
}

func (vm *VirtualMachine) Forward(listener net.Listener, network, address string) {
	vm.ForwardStopLatch(listener, network, address, nil)
}

func (vm *VirtualMachine) Shutdown(ctx context.Context) error {
	vm.UpdateStatus(ctx, event.StatusStopping)

	shutdown := util.Await(func() (struct{}, error) {
		err := vm.client.Shutdown(struct{}{}, nil)

		//nolint:wrapcheck
		return struct{}{}, err
	})

	log.Infof("shutting down listeners...")

	vm.mutex.Lock()
	status := vm.status

	var listenCh chan event.TypedEnvelope[event.Status]

	if status != event.StatusStopped {
		listenCh = make(chan event.TypedEnvelope[event.Status], 1)
		event.Listen(ctx, listenCh)
	}

	for listener := range vm.listeners {
		_ = listener.Close()
	}

	vm.listeners = map[net.Listener]struct{}{}
	vm.mutex.Unlock()

	log.Infof("awaiting guest shutdown...")

	if _, err := shutdown(); err != nil {
		return err
	}

	if err := vm.rpcConn.Close(); err != nil {
		return fmt.Errorf("failed to close rpc connection: %w", err)
	}

	log.Infof("waiting for vm to shutdown...")

	for statusEvent := range listenCh {
		log.Infof("status: %s", statusEvent.Event)
		if statusEvent.Event == event.StatusStopped {
			break
		}
	}

	return nil
}

func (vm *VirtualMachine) GC() error {
	//nolint:wrapcheck
	return vm.client.GC(struct{}{}, nil)
}
