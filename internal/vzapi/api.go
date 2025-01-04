package vzapi

import (
	"encoding/json"
	"io"
	"log"
	"net"
)

const APIPort = 1

type MessageType uint8

const (
	MessageTypeInfo MessageType = iota + 1
	MessageTypeShutdown
	MessageTypeMount
	MessageTypeWrite
	MessageTypeExec
	MessageTypeLaunch
	MessageTypeConnect
)

type InfoRequest struct {
	ProtocolVersion uint32 `json:"version"`
}

type InfoResponse struct {
	ProtocolVersion uint32 `json:"version"`
	IP              string `json:"ip"`
	IPv6            string `json:"ipv6"`
}

type MountRequest struct {
	FS            string   `json:"type"`
	Device        string   `json:"src"`
	Target        string   `json:"dst"`
	ReadOnly      bool     `json:"ro"`
	Options       string   `json:"opts"`
	FormatOptions []string `json:"mkfsargs"`
}

type WriteRequest struct {
	Path      string `json:"path"`
	Data      []byte `json:"data"`
	Base64    bool   `json:"b64"`
	Directory bool   `json:"dir"`
}

type LaunchRequest struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
	Env  []string `json:"env"`
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

	if size == 0 {
		_, err := conn.Write([]byte{0, 0, 0, 0})

		return err
	}

	if _, err := conn.Write([]byte{byte(size), byte(size >> 8), byte(size >> 16), byte(size >> 24)}); err != nil {
		return err
	}

	_, err := conn.Write(buf)

	return err
}

func writeMessage(conn net.Conn, typ MessageType, v any) error {
	log.Printf("Writing message type %d", typ)
	if _, err := conn.Write([]byte{byte(typ)}); err != nil {
		return err
	}

	if v == nil {
		log.Printf("No data to write")
		return nil
	}

	buf, err := json.Marshal(v)

	if err != nil {
		return err
	}

	log.Printf("Writing message: %s", string(buf))

	return write(conn, buf)
}
