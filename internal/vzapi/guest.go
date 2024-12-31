package vzapi

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
)

type Handler interface {
	Info(context.Context, InfoRequest) InfoResponse
	Shutdown(context.Context)
	Mount(context.Context, MountRequest) error
	Write(context.Context, WriteRequest) error
	Exec(context.Context, ExecRequest) (*Process, error)
	Connect(context.Context, ConnectRequest, net.Conn) (net.Conn, error)
}

type handler func(context.Context, net.Conn, Handler) error

func handleInfo(ctx context.Context, conn net.Conn, h Handler) error {
	buf, err := read(conn)

	if err != nil {
		return err
	}

	var req InfoRequest

	if err := json.Unmarshal(buf, &req); err != nil {
		return err
	}

	res := h.Info(ctx, req)

	bs, err := json.Marshal(res)

	if err != nil {
		return err
	}

	return write(conn, bs)
}

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
	stdoutCh := make(chan []byte, 1)
	stderrCh := make(chan []byte, 1)

	go proxyOutput(process.Stdout, stdoutCh, "stdout")
	go proxyOutput(process.Stderr, stderrCh, "stderr")

	defer close(stdoutCh)
	defer close(stderrCh)

	for {
		select {
		case bs := <-stdoutCh:
			log.Printf("writing stdout, len %d", len(bs))
			if _, err := conn.Write([]byte{byte(MessageTypeStdout)}); err != nil {
				return
			}

			if err := write(conn, bs); err != nil {
				return
			}
			log.Printf("wrote stdout")
		case bs := <-stderrCh:
			log.Printf("writing stderr, len %d", len(bs))
			if _, err := conn.Write([]byte{byte(MessageTypeStderr)}); err != nil {
				return
			}

			if err := write(conn, bs); err != nil {
				return
			}
			log.Printf("wrote stderr")
		case signal, ok := <-process.SignalSender:
			if !ok {
				return
			}
			log.Printf("writing signal")
			if _, err := conn.Write([]byte{byte(MessageTypeSignal), byte(signal)}); err != nil {
				return
			}
			log.Printf("wrote signal")
		}
	}
}

func proxyOutput(reader io.ReadCloser, outCh chan<- []byte, name string) {
	buf := make([]byte, 4096)

	for {
		n, err := reader.Read(buf)

		if err != nil {
			break
		}

		if n == 0 {
			panic("read 0 bytes")
		}

		log.Printf("read %d bytes", n)
		outCh <- buf[:n]
	}

	log.Printf("closing output channel: %s", name)
	outCh <- []byte{}
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
		case MessageTypeInfo:
			handler = handleInfo
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
