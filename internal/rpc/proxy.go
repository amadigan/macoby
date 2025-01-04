package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/rpc"
	"os/exec"

	applog "github.com/amadigan/macoby/internal/applog"
)

var log *applog.Logger = applog.New("rpc")

type ProxyRequest struct {
	Network    string   `json:"n"`
	Addr       string   `json:"a"`
	Args       []string `json:"r,omitempty"`
	Env        []string `json:"e,omitempty"`
	Dir        string   `json:"d,omitempty"`
	Input      []byte   `json:"i,omitempty"`
	PacketSize int      `json:"s,omitempty"`
}

type LaunchReceiver interface {
	Error(string, *struct{}) error
	Stdout([]byte, *struct{}) error
	Stderr([]byte, *struct{}) error
	Exit(int, *struct{}) error
}

type LaunchProcess interface {
	Start() error
	Wait() error
}

type launchWriter struct {
	*rpc.Client
	call string
}

func (w *launchWriter) Write(data []byte) (int, error) {
	err := w.Call(w.call, data, nil)

	if err != nil {
		return 0, err
	}

	return len(data), nil
}

type launchClient struct {
	*rpc.Client
}

func newLaunchClient(conn io.ReadWriteCloser) (client *launchClient, stdout io.Writer, stderr io.Writer) {
	return &launchClient{rpc.NewClient(conn)},
		&launchWriter{rpc.NewClient(conn), "LaunchReceiver.Stdout"},
		&launchWriter{rpc.NewClient(conn), "LaunchReceiver.Stderr"}
}

func (c *launchClient) Error(msg string) error {
	return c.Call("LaunchReceiver.Error", msg, nil)
}

func (c *launchClient) Exit(code int) error {
	return c.Call("LaunchReceiver.Exit", code, nil)
}

type ProxyHandler interface {
	Launch(r ProxyRequest, stdout io.Writer, stderr io.Writer) (LaunchProcess, error)
	DialTCP(addr string) (net.Conn, error)
	DialUDP(addr string) (net.PacketConn, net.Addr, error)
	DialUnix(addr string) (net.Conn, error)
	DialUnixgram(addr string) (net.PacketConn, net.Addr, error)
	Open(path string) (io.ReadCloser, error)
}

func ServeProxy(listener net.Listener, handler ProxyHandler) error {
	for {
		conn, err := listener.Accept()

		if err != nil {
			return err
		}

		go handleProxy(conn, handler)
	}
}

func handleProxy(conn net.Conn, handler ProxyHandler) {
	defer conn.Close()

	buf, err := readProxy(conn)

	if err != nil {
		log.Errorf("failed to read message: %v", err)
	}

	var req ProxyRequest

	if err := json.Unmarshal(buf, &req); err != nil {
		log.Errorf("failed to unmarshal message: %v", err)

		return
	}

	var sconn io.ReadWriteCloser
	var pconn net.PacketConn
	var raddr net.Addr

	switch req.Network {
	case "tcp":
		sconn, err = handler.DialTCP(req.Addr)
	case "udp":
		pconn, raddr, err = handler.DialUDP(req.Addr)
	case "unix":
		sconn, err = handler.DialUnix(req.Addr)
	case "unixgram":
		pconn, raddr, err = handler.DialUnixgram(req.Addr)
	case "file":
		var f io.ReadCloser

		f, err = handler.Open(req.Addr)

		if err == nil {
			defer f.Close()

			writeProxy(conn, []byte{})

			_, err = io.Copy(conn, f)
		}
	case "exec":
		client, stdout, stderr := newLaunchClient(conn)

		proc, err := handler.Launch(req, stdout, stderr)

		if err != nil {
			if cerr := client.Error(err.Error()); cerr != nil {
				log.Errorf("failed to send launch error: %w: %v", cerr, err)
			}

			return
		}

		if err := proc.Start(); err != nil {
			if cerr := client.Error(err.Error()); cerr != nil {
				log.Errorf("failed to send start error: %w: %v", cerr, err)
			}

			return
		}

		if err := proc.Wait(); err != nil {
			var exit *exec.ExitError

			if errors.As(err, &exit) {
				if cerr := client.Exit(exit.ExitCode()); cerr != nil {
					log.Errorf("failed to send exit code: %w: %v", cerr, exit)
				}
			} else {
				if cerr := client.Error(err.Error()); cerr != nil {
					log.Errorf("failed to send error: %w: %v", cerr, err)
				}
			}
		}

	}

	if err != nil {
		log.Errorf("failed to dial: %v", err)

		return
	}

	if sconn != nil {
		defer sconn.Close()

		go func() {
			_, _ = io.Copy(sconn, conn)
		}()
		_, _ = io.Copy(conn, sconn)

		return
	}

	defer pconn.Close()
	size := req.PacketSize

	if size == 0 {
		size = 2 << 16
	}

	done := make(chan struct{})

	go func() {
		defer close(done)
		buf := make([]byte, size)

		for {
			n, _, err := pconn.ReadFrom(buf)

			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					log.Errorf("failed to read packet: %v", err)
				}

				return
			}

			if err := writeProxy(conn, buf[:n]); err != nil {
				log.Errorf("failed to write packet: %v", err)

				return
			}
		}
	}()

	for {
		buf, err := readProxy(conn)

		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				log.Errorf("failed to read message: %v", err)
			}

			break
		}

		if _, err := pconn.WriteTo(buf, raddr); err != nil {
			log.Errorf("failed to write packet: %v", err)

			return
		}
	}

	<-done

}

func readProxy(conn io.Reader) ([]byte, error) {
	buf := make([]byte, 2)

	if _, err := conn.Read(buf); err != nil {
		return nil, fmt.Errorf("failed to read message size: %v", err)
	}

	size := int(buf[0])<<8 | int(buf[1])

	buf = make([]byte, size)

	if _, err := conn.Read(buf); err != nil {
		return nil, fmt.Errorf("failed to read message: %v", err)
	}

	return buf, nil
}

func writeProxy(conn io.Writer, data []byte) error {
	size := len(data)

	if size > 0xffff {
		return fmt.Errorf("message too large: %d", size)
	}

	buf := []byte{byte(size >> 8), byte(size)}

	if _, err := conn.Write(buf); err != nil {
		return fmt.Errorf("failed to write message size: %v", err)
	}

	if size > 0 {
		if _, err := conn.Write(data); err != nil {
			return fmt.Errorf("failed to write message: %v", err)
		}
	}

	return nil
}

type ProxyClient struct {
	conn net.Conn
}

func NewProxyClient(conn net.Conn) *ProxyClient {
	return &ProxyClient{conn}
}

func (c *ProxyClient) Dial(network, address string) (net.Conn, error) {
	req := ProxyRequest{
		Network: network,
		Addr:    address,
	}

	buf, _ := json.Marshal(req)

	if err := writeProxy(c.conn, buf); err != nil {
		return nil, fmt.Errorf("failed to write request: %v", err)
	}

	buf, err := readProxy(c.conn)

	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	if len(buf) > 0 {
		return nil, fmt.Errorf("failed to dial: %s", string(buf))
	}

	return c.conn, nil
}

func (c *ProxyClient) DialPacket(network string, address string, psize int) (net.PacketConn, net.Addr, error) {
	// TODO: implement

	return nil, nil, fmt.Errorf("not implemented")
}

func (c *ProxyClient) Open(path string) (io.ReadCloser, error) {
	req := ProxyRequest{
		Network: "file",
		Addr:    path,
	}

	buf, _ := json.Marshal(req)

	if err := writeProxy(c.conn, buf); err != nil {
		return nil, fmt.Errorf("failed to write request: %v", err)
	}

	buf, err := readProxy(c.conn)

	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	if len(buf) > 0 {
		return nil, fmt.Errorf("failed to open: %s", string(buf))
	}

	return c.conn, nil
}

func (c *ProxyClient) Launch(req ProxyRequest, rcvr LaunchReceiver) error {
	req.Network = "exec"

	buf, _ := json.Marshal(req)

	if err := writeProxy(c.conn, buf); err != nil {
		return fmt.Errorf("failed to write request: %v", err)
	}

	server := rpc.NewServer()
	server.RegisterName("LaunchReceiver", rcvr)

	server.ServeConn(c.conn)

	return nil
}
