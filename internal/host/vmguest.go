package host

import (
	"net"
	gorpc "net/rpc"

	"github.com/amadigan/macoby/internal/rpc"
)

func (vm *VirtualMachine) Write(path string, data []byte) error {
	return vm.client.Write(rpc.WriteRequest{Path: path, Data: data}, nil)
}

func (vm *VirtualMachine) Mkdir(path string) error {
	return vm.client.Mkdir(path, nil)
}

func (vm *VirtualMachine) Mount(source, target, fstype string, flags []string) error {
	return vm.client.Mount(rpc.MountRequest{FS: fstype, Device: source, Target: target, Flags: flags}, nil)
}

func (vm *VirtualMachine) Run(req rpc.Command) (rpc.CommandOutput, error) {
	var out rpc.CommandOutput
	err := vm.client.Run(req, &out)
	return out, err
}

func (vm *VirtualMachine) Launch(req rpc.Command) (int64, error) {
	var pid int64
	err := vm.client.Launch(req, &pid)
	return pid, err
}

func (vm *VirtualMachine) Listen(network, address string) error {
	return vm.client.Listen(rpc.ListenRequest{Network: network, Address: address}, nil)
}

func (vm *VirtualMachine) Signal(pid int64, sig int) error {
	return vm.client.Signal(rpc.SignalRequest{Pid: pid, Signal: sig}, nil)
}

func (vm *VirtualMachine) Metrics(names []string) (rpc.Metrics, error) {
	var metrics rpc.Metrics
	err := vm.client.Metrics(names, &metrics)
	return metrics, err
}

func (vm *VirtualMachine) DialUDP(network string, laddr *net.UDPAddr, raddr *net.UDPAddr) (net.PacketConn, error) {
	conn, err := vm.vsock.Connect(1)
	if err != nil {
		return nil, err
	}

	return rpc.DialUDP(gorpc.NewClient(conn), network, laddr, raddr)
}

func (vm *VirtualMachine) ListenUDP(network string, laddr *net.UDPAddr) (net.PacketConn, error) {
	conn, err := vm.vsock.Connect(1)
	if err != nil {
		return nil, err
	}

	return rpc.ListenUDP(gorpc.NewClient(conn), network, laddr)
}

func (vm *VirtualMachine) DialUnixgram(network string, laddr *net.UnixAddr, raddr *net.UnixAddr) (net.PacketConn, error) {
	conn, err := vm.vsock.Connect(1)
	if err != nil {
		return nil, err
	}

	return rpc.DialUnix(gorpc.NewClient(conn), network, laddr, raddr)
}

func (vm *VirtualMachine) ListenUnixgram(network string, laddr *net.UnixAddr) (net.PacketConn, error) {
	conn, err := vm.vsock.Connect(1)
	if err != nil {
		return nil, err
	}

	return rpc.ListenUnix(gorpc.NewClient(conn), network, laddr)
}

func (vm *VirtualMachine) Dial(network, address string) (net.Conn, error) {
	conn, err := vm.vsock.Connect(2)
	if err != nil {
		return nil, err
	}

	return rpc.Dial(conn, network, address)
}
