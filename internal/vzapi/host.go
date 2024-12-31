package vzapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
)

type Client struct {
	Conn net.Conn
}

func (c *Client) Info(r InfoRequest) (InfoResponse, error) {
	if err := writeMessage(c.Conn, MessageTypeInfo, r); err != nil {
		return InfoResponse{}, err
	}

	buf, err := read(c.Conn)

	if err != nil {
		return InfoResponse{}, err
	}

	var res InfoResponse

	if err := json.Unmarshal(buf, &res); err != nil {
		return InfoResponse{}, err
	}

	return res, nil
}

func (c *Client) Shutdown() error {
	return writeMessage(c.Conn, MessageTypeShutdown, nil)
}

func (c *Client) Mount(req MountRequest) error {
	if err := writeMessage(c.Conn, MessageTypeMount, req); err != nil {
		return err
	}

	errstr, err := c.readError()

	if err != nil {
		return err
	}

	if errstr != "" {
		return fmt.Errorf("mount failed: %s", errstr)
	}

	return nil
}

func (c *Client) readError() (string, error) {
	buf, err := read(c.Conn)

	if err != nil {
		return "", err
	}

	if len(buf) == 0 {
		return "", nil
	}

	return string(buf), nil
}

func (c *Client) Write(req WriteRequest) error {
	if err := writeMessage(c.Conn, MessageTypeWrite, req); err != nil {
		return err
	}

	errstr, err := c.readError()

	if err != nil {
		return err
	}

	if errstr != "" {
		return fmt.Errorf("write failed: %s", errstr)
	}

	return nil
}

type RemoteProcess struct {
	Stdin          io.Writer
	Stdout         io.Reader
	Stderr         io.Reader
	SignalReceiver <-chan int8
	SignalSender   chan<- int8
}

func (c *Client) Exec(req ExecRequest) (*RemoteProcess, error) {
	if err := writeMessage(c.Conn, MessageTypeExec, req); err != nil {
		return nil, err
	}

	errstr, err := c.readError()

	if err != nil {
		return nil, err
	}

	if errstr != "" {
		return nil, fmt.Errorf("exec failed: %s", errstr)
	}

	soutR, soutW := io.Pipe()
	serrR, serrW := io.Pipe()
	sigOut := make(chan int8, 1)
	sigIn := make(chan int8, 1)
	sinCh := make(chan []byte, 1)

	process := &RemoteProcess{
		Stdin:          &remoteStdin{msgChan: sinCh},
		Stdout:         soutR,
		Stderr:         serrR,
		SignalReceiver: sigOut,
		SignalSender:   sigIn,
	}

	go handleRemoteOut(soutW, serrW, sigOut, c.Conn)

	go func() {
		for {
			select {
			case bs := <-sinCh:
				if err := writeMessage(c.Conn, MessageTypeStdin, bs); err != nil {
					return
				}
			case signal, ok := <-sigIn:
				if !ok {
					return
				}

				if _, err := c.Conn.Write([]byte{byte(MessageTypeSignal), byte(signal)}); err != nil {
					return
				}
			}
		}
	}()

	return process, nil
}

type remoteStdin struct {
	msgChan chan []byte
}

func (r *remoteStdin) Write(bs []byte) (int, error) {
	r.msgChan <- bs

	return len(bs), nil
}

func handleRemoteOut(stdout io.WriteCloser, stderr io.WriteCloser, sigchan chan<- int8, conn net.Conn) {
	buf := make([]byte, 1)

	outClosed := false
	errClosed := false

	for {
		if _, err := conn.Read(buf); err != nil {
			break
		}

		typ := MessageType(buf[0])

		if typ == MessageTypeSignal {
			if _, err := conn.Read(buf); err != nil {
				break
			}

			signal := int8(buf[0])

			sigchan <- signal
		} else if typ == MessageTypeStdout {
			bs, err := read(conn)

			if err != nil {
				break
			}

			if len(bs) == 0 {
				stdout.Close()
				outClosed = true

				continue
			}

			if _, err := stdout.Write(bs); err != nil {
				break
			}
		} else if typ == MessageTypeStderr {
			bs, err := read(conn)

			if err != nil {
				break
			}

			if len(bs) == 0 {
				stderr.Close()
				errClosed = true

				continue
			}

			if _, err := stderr.Write(bs); err != nil {
				break
			}
		}
	}

	if !outClosed {
		stdout.Close()
	}

	if !errClosed {
		stderr.Close()
	}
}

func (c *Client) Connect(network, address string) (net.Conn, error) {
	if err := writeMessage(c.Conn, MessageTypeConnect, ConnectRequest{Network: network, Address: address}); err != nil {
		return nil, err
	}

	errstr, err := c.readError()

	if err != nil {
		return nil, err
	}

	if errstr != "" {
		return nil, fmt.Errorf("connect failed: %s", errstr)
	}

	return c.Conn, nil
}
