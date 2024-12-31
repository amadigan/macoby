package vzapi

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
)

type Handler interface {
	Shutdown(context.Context)
	Mount(context.Context, MountRequest) error
	Write(context.Context, WriteRequest) error
	Exec(context.Context, ExecRequest) (*Process, error)
	Connect(context.Context, ConnectRequest, net.Conn) (net.Conn, error)
}

type handler func(context.Context, net.Conn, Handler) error

func handleShutdown(ctx context.Context, _ net.Conn, h Handler) error {
	h.Shutdown(ctx)

	return nil
}

func writeError(conn net.Conn, err error) error {
	if err == nil {
		return write(conn, nil)
	}

	if str := err.Error(); str != "" {
		return write(conn, []byte(str))
	}

	return write(conn, []byte("unknown error"))
}

func handleMount(ctx context.Context, conn net.Conn, h Handler) error {
	buf, err := read(conn)

	if err != nil {
		return err
	}

	var req MountRequest

	if err := json.Unmarshal(buf, &req); err != nil {
		return err
	}

	err = h.Mount(ctx, req)

	return writeError(conn, err)
}

func handleWrite(ctx context.Context, conn net.Conn, h Handler) error {
	buf, err := read(conn)

	if err != nil {
		return err
	}

	var req WriteRequest

	if err := json.Unmarshal(buf, &req); err != nil {
		return err
	}

	err = h.Write(ctx, req)

	return writeError(conn, err)
}

func handleExec(ctx context.Context, conn net.Conn, h Handler) error {
	buf, err := read(conn)

	if err != nil {
		return err
	}

	var req ExecRequest

	if err := json.Unmarshal(buf, &req); err != nil {
		return err
	}

	process, err := h.Exec(ctx, req)

	if wrErr := writeError(conn, err); wrErr != nil {
		return wrErr
	}

	if err != nil {
		return nil
	}

	go handleProcessInput(conn, process)

	handleProcessOutput(conn, process)

	return nil
}

func handleProcessInput(conn net.Conn, process *Process) {
	buf := make([]byte, 1)

	defer process.Stdin.Close()

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

			process.SignalReceiver <- signal
		} else if typ == MessageTypeStdin {
			bs, err := read(conn)

			if err != nil {
				break
			}

			if _, err := process.Stdin.Write(bs); err != nil {
				break
			}
		}
	}
}

func handleProcessOutput(conn net.Conn, process *Process) {
	stdoutCh := make(chan []byte)
	stderrCh := make(chan []byte)

	go proxyOutput(process.Stdout, stdoutCh)
	go proxyOutput(process.Stderr, stderrCh)

	for {
		select {
		case bs := <-stdoutCh:
			if err := writeMessage(conn, MessageTypeStdout, bs); err != nil {
				return
			}
		case bs := <-stderrCh:
			if err := writeMessage(conn, MessageTypeStderr, bs); err != nil {
				return
			}
		case signal, ok := <-process.SignalSender:
			if !ok {
				return
			}

			if _, err := conn.Write([]byte{byte(MessageTypeSignal), byte(signal)}); err != nil {
				return
			}
		}
	}

}

func proxyOutput(reader io.ReadCloser, outCh chan<- []byte) {
	buf := make([]byte, 4096)

	defer close(outCh)

	for {
		n, err := reader.Read(buf)

		if err != nil {
			break
		}

		outCh <- buf[:n]
	}

	outCh <- nil
}

func handleConnect(ctx context.Context, conn net.Conn, h Handler) error {
	buf, err := read(conn)

	if err != nil {
		return err
	}

	var req ConnectRequest

	if err := json.Unmarshal(buf, &req); err != nil {
		return err
	}

	remote, err := h.Connect(ctx, req, conn)

	if err != nil {
		writeError(conn, err)

		return nil
	}

	defer remote.Close()

	done := make(chan struct{})

	go func() {
		write(conn, nil)
		io.Copy(conn, remote)
		close(done)
	}()

	io.Copy(remote, conn)
	<-done

	return nil
}

func Handle(ctx context.Context, conn net.Conn, h Handler) {
	buf := make([]byte, 1)

	defer conn.Close()

	for {
		if _, err := conn.Read(buf); err != nil {
			break
		}

		typ := MessageType(buf[0])

		var handler handler
		term := false

		switch typ {
		case MessageTypeShutdown:
			handler = handleShutdown
		case MessageTypeMount:
			handler = handleMount
		case MessageTypeWrite:
			handler = handleWrite
		case MessageTypeExec:
			handler = handleExec
			term = true
		case MessageTypeConnect:
			handler = handleConnect
			term = true
		default:
			continue
		}

		if err := handler(ctx, conn, h); err != nil {
			log.Printf("error handling message: %v", err)
			break
		}

		if term {
			break
		}
	}
}
