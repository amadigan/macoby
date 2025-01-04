package vzapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
)

type Client struct {
	Conn net.Conn
}

func (c *Client) Info(_ context.Context, r InfoRequest) (InfoResponse, error) {
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

func (c *Client) Shutdown(_ context.Context) error {
	return writeMessage(c.Conn, MessageTypeShutdown, nil)
}

func (c *Client) Mount(_ context.Context, req MountRequest) error {
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

func (c *Client) Write(_ context.Context, req WriteRequest) error {
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

func (c *Client) DialContext(_ context.Context, network string, address string) (net.Conn, error) {
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

func (c *Client) Launch(_ context.Context, req LaunchRequest) error {
	if err := writeMessage(c.Conn, MessageTypeLaunch, req); err != nil {
		return err
	}

	errstr, err := c.readError()

	if err != nil {
		return err
	}

	if errstr != "" {
		return fmt.Errorf("launch failed: %s", errstr)
	}

	return nil
}
