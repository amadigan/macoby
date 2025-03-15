package host

import (
	"net"
	"strconv"
	"sync"

	"github.com/amadigan/macoby/internal/util"
)

type Listener struct {
	addrs []net.IP
	VM    *VirtualMachine
	ports map[string][]net.Listener

	mutex sync.Mutex
}

func NewListener(bind string) (*Listener, error) {
	if bind == "" {
		bind = "*"
	}

	listener := &Listener{ports: make(map[string][]net.Listener)}

	if bind == "*" {
		log.Infof("listening on all interfaces")
		listener.addrs = []net.IP{net.IPv6zero}

		return listener, nil
	}

	if addr := net.ParseIP(bind); addr != nil {
		log.Infof("listening on %s", addr)
		listener.addrs = []net.IP{addr}

		return listener, nil
	}

	iface, err := net.InterfaceByName(bind)
	if err != nil {
		return nil, err
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}

	log.Infof("listening on interface %s", iface.Name)

	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			return nil, err
		}

		listener.addrs = append(listener.addrs, ip)
	}

	return listener, nil
}

type ListenerProto string

const (
	ListenerProtoTCP ListenerProto = "tcp"
	ListenerProtoUDP ListenerProto = "udp"
)

type GuestPort struct {
	Proto ListenerProto
	Port  int
}

func (l *Listener) Forward(id string, guestIP net.IP, ports map[GuestPort]util.Set[int]) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	var listeners []net.Listener

	for guestPort, hostPorts := range ports {
		raddr := guestIP.String() + ":" + strconv.Itoa(guestPort.Port)
		if guestPort.Proto != ListenerProtoUDP {
			for hostPort := range hostPorts {
				for _, addr := range l.addrs {
					listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: addr, Port: hostPort})
					if err != nil {
						log.Errorf("failed to listen on %s:%d: %v", addr, hostPort, err)

						continue
					}

					log.Infof("forwarding %s to %s", listener.Addr(), raddr)
					go l.VM.Forward(listener, "tcp", raddr)
					listeners = append(listeners, listener)
				}
			}
		} else {
			log.Warnf("ignoring UDP port forwarding")
		}
	}

	l.ports[id] = listeners
}

func (l *Listener) Close(id string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	for _, listener := range l.ports[id] {
		_ = listener.Close()
	}

	delete(l.ports, id)
}

func (l *Listener) CloseAll() {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	for _, listeners := range l.ports {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}
}
