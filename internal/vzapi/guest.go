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
	Launch(context.Context, LaunchRequest) error
	Connect(context.Context, ConnectRequest) (net.Conn, error)
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

func handleLaunch(ctx context.Context, conn net.Conn, h Handler) error {
	buf, err := read(conn)

	if err != nil {
		return err
	}

	var req LaunchRequest

	if err := json.Unmarshal(buf, &req); err != nil {
		return err
	}

	err = h.Launch(ctx, req)

	return writeError(conn, err)
}

func handleConnect(ctx context.Context, conn net.Conn, h Handler) error {
	log.Printf("connect request")
	buf, err := read(conn)

	if err != nil {
		return err
	}

	var req ConnectRequest

	if err := json.Unmarshal(buf, &req); err != nil {
		return err
	}

	log.Printf("connect request: %v", req)

	remote, err := h.Connect(ctx, req)

	if err != nil {
		log.Printf("error connecting: %v", err)
		writeError(conn, err)

		return nil
	}

	defer remote.Close()

	write(conn, []byte{})

	go func() {
		io.Copy(remote, conn)
	}()

	_, err = io.Copy(conn, remote)

	if err != nil {
		log.Printf("error copying from remote to local: %v", err)
	}

	log.Printf("remote output closed")

	return nil
}

func Handle(ctx context.Context, conn net.Conn, h Handler) {
	buf := make([]byte, 1)

	defer conn.Close()

	for {
		log.Printf("waiting for message")

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
		case MessageTypeLaunch:
			handler = handleLaunch
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

	log.Printf("connection closed")
}
