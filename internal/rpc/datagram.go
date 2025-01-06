package rpc

import (
	"fmt"
	"net"
	"net/rpc"
	"sync"
	"time"
)

type DatagramSpec struct {
	Network    string
	LocalUDP   *net.UDPAddr
	RemoteUDP  *net.UDPAddr
	LocalUnix  *net.UnixAddr
	RemoteUnix *net.UnixAddr
}

type Datagram struct {
	UDPAddr  *net.UDPAddr
	UnixAddr *net.UnixAddr
	Data     []byte
}

type DatagramProxy struct {
	udp *net.UDPConn
	unx *net.UnixConn

	mutex sync.RWMutex
}

func (d *DatagramProxy) Dial(spec DatagramSpec, _ *struct{}) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.udp != nil || d.unx != nil {
		return fmt.Errorf("already connected")
	}

	var err error

	if spec.RemoteUDP != nil {
		d.udp, err = net.DialUDP(spec.Network, spec.LocalUDP, spec.RemoteUDP)
	} else {
		d.unx, err = net.DialUnix(spec.Network, spec.LocalUnix, spec.RemoteUnix)
	}

	return err
}

func (d *DatagramProxy) Listen(spec DatagramSpec, _ *struct{}) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.udp != nil || d.unx != nil {
		return fmt.Errorf("already connected")
	}

	var err error

	if spec.RemoteUDP != nil {
		d.udp, err = net.ListenUDP(spec.Network, spec.LocalUDP)
	} else {
		d.unx, err = net.ListenUnixgram(spec.Network, spec.LocalUnix)
	}

	return err
}

func (d *DatagramProxy) Read(size int, out *Datagram) error {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	buf := make([]byte, size)

	if d.udp != nil {
		n, addr, err := d.udp.ReadFromUDP(buf)
		if err != nil {
			return err
		}

		out.UDPAddr = addr
		out.Data = buf[:n]
	} else {
		n, addr, err := d.unx.ReadFromUnix(buf)
		if err != nil {
			return err
		}

		out.UnixAddr = addr
		out.Data = buf[:n]
	}

	return nil
}

func (d *DatagramProxy) Write(datagram Datagram, _ *struct{}) (err error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.udp != nil {
		_, err = d.udp.WriteToUDP(datagram.Data, datagram.UDPAddr)
	} else {
		_, err = d.unx.WriteToUnix(datagram.Data, datagram.UnixAddr)
	}

	return
}

func (d *DatagramProxy) SetDeadline(t time.Time, _ *struct{}) (err error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.udp != nil {
		err = d.udp.SetDeadline(t)
	} else {
		err = d.unx.SetDeadline(t)
	}

	return
}

func (d *DatagramProxy) SetReadDeadline(t time.Time, _ *struct{}) (err error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.udp != nil {
		err = d.udp.SetReadDeadline(t)
	} else {
		err = d.unx.SetReadDeadline(t)
	}

	return
}

func (d *DatagramProxy) SetWriteDeadline(t time.Time, _ *struct{}) (err error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.udp != nil {
		err = d.udp.SetWriteDeadline(t)
	} else {
		err = d.unx.SetWriteDeadline(t)
	}

	return
}

func (d *DatagramProxy) Close(_ struct{}, _ *struct{}) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.udp != nil {
		d.udp.Close()
		d.udp = nil
	}

	if d.unx != nil {
		d.unx.Close()
		d.unx = nil
	}

	return nil
}

func ServeDatagramProxy(conn net.Conn) {
	server := rpc.NewServer()
	proxy := &DatagramProxy{}

	if err := server.Register(proxy); err != nil {
		panic(err)
	}

	server.ServeConn(conn)
	proxy.Close(struct{}{}, &struct{}{})
}

type DatagramClient struct {
	client    *rpc.Client
	localAddr net.Addr
}

var _ net.PacketConn = &DatagramClient{}

func (d *DatagramClient) ReadFrom(b []byte) (int, net.Addr, error) {
	var datagram Datagram

	if err := d.client.Call("DatagramProxy.Read", len(b), &datagram); err != nil {
		return 0, nil, err
	}

	n := copy(b, datagram.Data)

	if datagram.UDPAddr != nil {
		return n, datagram.UDPAddr, nil
	}

	return n, datagram.UnixAddr, nil
}

func (d *DatagramClient) Read(size int) ([]byte, net.Addr, error) {
	var datagram Datagram

	if err := d.client.Call("DatagramProxy.Read", size, &datagram); err != nil {
		return nil, nil, err
	}

	if datagram.UDPAddr != nil {
		return datagram.Data, datagram.UDPAddr, nil
	}

	return datagram.Data, datagram.UnixAddr, nil
}

func (d *DatagramClient) Close() error {
	d.client.Call("DatagramProxy.Close", struct{}{}, nil)
	return d.client.Close()
}

func (d *DatagramClient) LocalAddr() net.Addr {
	return d.localAddr
}

func (d *DatagramClient) SetDeadline(t time.Time) error {
	return d.client.Call("DatagramProxy.SetDeadline", t, nil)
}

func (d *DatagramClient) SetReadDeadline(t time.Time) error {
	return d.client.Call("DatagramProxy.SetReadDeadline", t, nil)
}

func (d *DatagramClient) SetWriteDeadline(t time.Time) error {
	return d.client.Call("DatagramProxy.SetWriteDeadline", t, nil)
}

func (d *DatagramClient) WriteTo(b []byte, addr net.Addr) (int, error) {
	datagram := Datagram{Data: b}

	if udpAddr, ok := addr.(*net.UDPAddr); ok {
		datagram.UDPAddr = udpAddr
	} else {
		datagram.UnixAddr = addr.(*net.UnixAddr)
	}

	if err := d.client.Call("DatagramProxy.Write", datagram, nil); err != nil {
		return 0, err
	}

	return len(b), nil
}

func DialUDP(client *rpc.Client, network string, laddr *net.UDPAddr, raddr *net.UDPAddr) (net.PacketConn, error) {
	spec := DatagramSpec{
		Network:   network,
		LocalUDP:  laddr,
		RemoteUDP: raddr,
	}

	if err := client.Call("DatagramProxy.Dial", spec, nil); err != nil {
		return nil, err
	}

	return &DatagramClient{client: client, localAddr: laddr}, nil
}

func ListenUDP(client *rpc.Client, network string, laddr *net.UDPAddr) (net.PacketConn, error) {
	spec := DatagramSpec{
		Network:  network,
		LocalUDP: laddr,
	}

	if err := client.Call("DatagramProxy.Listen", spec, nil); err != nil {
		return nil, err
	}

	return &DatagramClient{client: client, localAddr: laddr}, nil
}

func DialUnix(client *rpc.Client, network string, laddr *net.UnixAddr, raddr *net.UnixAddr) (net.PacketConn, error) {
	spec := DatagramSpec{
		Network:    network,
		LocalUnix:  laddr,
		RemoteUnix: raddr,
	}

	if err := client.Call("DatagramProxy.Dial", spec, nil); err != nil {
		return nil, err
	}

	return &DatagramClient{client: client, localAddr: laddr}, nil
}

func ListenUnix(client *rpc.Client, network string, laddr *net.UnixAddr) (net.PacketConn, error) {
	spec := DatagramSpec{
		Network:   network,
		LocalUnix: laddr,
	}

	if err := client.Call("DatagramProxy.Listen", spec, nil); err != nil {
		return nil, err
	}

	return &DatagramClient{client: client, localAddr: laddr}, nil
}
