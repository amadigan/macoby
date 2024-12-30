package vzapi

import (
	"encoding/json"
	"io"
	"net"
)

const APIPort = 1

type MessageType uint8

const (
	MessageTypeShutdown MessageType = iota + 1
	MessageTypeMount
	MessageTypeWrite
	MessageTypeExec
	MessageTypeSignal
	MessageTypeStdin
	MessageTypeStdout
	MessageTypeStderr
	MessageTypeConnect
)

type MountRequest struct {
	FS       string `json:"type"`
	Device   string `json:"src"`
	Target   string `json:"dst"`
	ReadOnly bool   `json:"ro"`
}

type WriteRequest struct {
	Path      string `json:"path"`
	Data      []byte `json:"data"`
	Base64    bool   `json:"b64"`
	Directory bool   `json:"dir"`
}

type ExecRequest struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
	Env  []string `json:"env"`
}

type Process struct {
	Stdin          io.WriteCloser
	Stdout         io.ReadCloser
	Stderr         io.ReadCloser
	SignalReceiver chan<- int8
	SignalSender   <-chan int8
}

type ConnectRequest struct {
	Network string `json:"network"`
	Address string `json:"address"`
}

func read(conn net.Conn) ([]byte, error) {
	buf := make([]byte, 4)

	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}

	size := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16 | int(buf[3])<<24

	buf = make([]byte, size)

	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

func write(conn net.Conn, buf []byte) error {
	size := len(buf)

	if size > 0xffffffff {
		return io.ErrShortWrite
	}

	if _, err := conn.Write([]byte{byte(size), byte(size >> 8), byte(size >> 16), byte(size >> 24)}); err != nil {
		return err
	}

	_, err := conn.Write(buf)

	return err
}

func writeMessage(conn net.Conn, typ MessageType, v any) error {
	if _, err := conn.Write([]byte{byte(typ)}); err != nil {
		return err
	}

	if v == nil {
		return nil
	}

	buf, err := json.Marshal(v)

	if err != nil {
		return err
	}

	return write(conn, buf)
}
