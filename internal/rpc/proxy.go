package rpc

import (
	"fmt"
	"io"
	"net"
	"strings"

	applog "github.com/amadigan/macoby/internal/applog"
)

var log *applog.Logger = applog.New("rpc")

func ServeStreamProxy(conn net.Conn) {
	defer conn.Close()
	addr, err := readProxy(conn)

	if err != nil {
		log.Errorf("Failed to read proxy address: %v", err)

		return
	}

	parts := strings.SplitN(addr, ":", 2)

	if len(parts) != 2 {
		_ = writeProxy(conn, "invalid address")

		return
	}

	out, err := net.Dial(parts[0], parts[1])

	if err != nil {
		_ = writeProxy(conn, err.Error())

		return
	}

	defer out.Close()

	if err := writeProxy(conn, ""); err != nil {
		log.Errorf("Failed to write proxy response: %v", err)
	}

	go func() {
		_, _ = io.Copy(out, conn)
	}()

	_, _ = io.Copy(conn, out)
}

func readProxy(conn net.Conn) (string, error) {
	buf := make([]byte, 2)

	if _, err := conn.Read(buf); err != nil {
		return "", fmt.Errorf("failed to read proxy size: %w", err)
	}

	size := int(buf[0])<<8 | int(buf[1])

	buf = make([]byte, size)

	if _, err := conn.Read(buf); err != nil {
		return "", fmt.Errorf("failed to read proxy data: %w", err)
	}

	return string(buf), nil
}

func writeProxy(conn net.Conn, data string) error {
	size := len(data)
	buf := []byte{byte(size >> 8), byte(size)}

	if _, err := conn.Write(buf); err != nil {
		return fmt.Errorf("failed to write proxy size: %w", err)
	}

	if _, err := conn.Write([]byte(data)); err != nil {
		return fmt.Errorf("failed to write proxy data: %w", err)
	}

	return nil
}

func Dial(proxy net.Conn, network string, addr string) (net.Conn, error) {
	if err := writeProxy(proxy, network+":"+addr); err != nil {
		return nil, err
	}

	resp, err := readProxy(proxy)

	if err != nil {
		return nil, err
	}

	if resp != "" {
		return nil, net.UnknownNetworkError(resp)
	}

	return proxy, nil
}
