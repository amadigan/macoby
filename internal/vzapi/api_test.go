package vzapi

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockHandler struct {
	ShutdownFunc func(context.Context)
	MountFunc    func(context.Context, MountRequest) error
	WriteFunc    func(context.Context, WriteRequest) error
	ExecFunc     func(context.Context, ExecRequest) (*Process, error)
	ConnectFunc  func(context.Context, ConnectRequest, net.Conn) (net.Conn, error)
}

func (m *mockHandler) Shutdown(ctx context.Context) {
	if m.ShutdownFunc != nil {
		m.ShutdownFunc(ctx)
	}
}

func (m *mockHandler) Mount(ctx context.Context, req MountRequest) error {
	if m.MountFunc != nil {
		return m.MountFunc(ctx, req)
	}

	return nil
}

func (m *mockHandler) Write(ctx context.Context, req WriteRequest) error {
	if m.WriteFunc != nil {
		return m.WriteFunc(ctx, req)
	}

	return nil
}

func (m *mockHandler) Exec(ctx context.Context, req ExecRequest) (*Process, error) {
	if m.ExecFunc != nil {
		return m.ExecFunc(ctx, req)
	}

	return nil, nil
}

func (m *mockHandler) Connect(ctx context.Context, req ConnectRequest, conn net.Conn) (net.Conn, error) {
	if m.ConnectFunc != nil {
		return m.ConnectFunc(ctx, req, conn)
	}

	return nil, nil
}

func setup(handler Handler) Client {
	left, right := net.Pipe()

	go Handle(context.Background(), right, handler)

	return Client{Conn: left}
}

func TestShutdown(t *testing.T) {
	rcChan := make(chan struct{})

	handler := &mockHandler{
		ShutdownFunc: func(_ context.Context) {
			close(rcChan)
		},
	}

	c := setup(handler)

	if err := c.Shutdown(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)

	defer cancel()

	select {
	case <-rcChan:
		return
	case <-ctx.Done():
		t.Fatal("timeout")
	}
}

func TestMount(t *testing.T) {
	rcChan := make(chan MountRequest, 1)

	defer close(rcChan)

	handler := &mockHandler{
		MountFunc: func(_ context.Context, req MountRequest) error {
			rcChan <- req
			return nil
		},
	}

	c := setup(handler)

	req := MountRequest{
		FS:       "ext4",
		Device:   "vdb",
		Target:   "/media/data",
		ReadOnly: true,
	}

	if err := c.Mount(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)

	defer cancel()

	select {
	case got := <-rcChan:
		assert.Equal(t, req, got)

		return
	case <-ctx.Done():
		t.Fatal("timeout")
	}
}

func TestWrite(t *testing.T) {
	rcChan := make(chan WriteRequest, 1)

	defer close(rcChan)

	handler := &mockHandler{
		WriteFunc: func(_ context.Context, req WriteRequest) error {
			rcChan <- req
			return nil
		},
	}

	c := setup(handler)

	req := WriteRequest{
		Path: "/etc/hosts",
		Data: []byte("localhost\t127.0.0.1\n"),
	}

	if err := c.Write(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)

	defer cancel()

	select {
	case got := <-rcChan:
		assert.Equal(t, req, got)

		return
	case <-ctx.Done():
		t.Fatal("timeout")
	}
}

func TestExec(t *testing.T) {
	rcChan := make(chan ExecRequest, 1)

	defer close(rcChan)

	process := &Process{
		Stdin:          &mockStream{Buffer: &bytes.Buffer{}},
		Stdout:         &mockStream{Buffer: bytes.NewBufferString("stdout")},
		Stderr:         &mockStream{Buffer: bytes.NewBufferString("stderr")},
		SignalReceiver: make(chan int8),
		SignalSender:   make(chan int8),
	}

	handler := &mockHandler{
		ExecFunc: func(_ context.Context, req ExecRequest) (*Process, error) {
			rcChan <- req

			return process, nil
		},
	}

	c := setup(handler)

	req := ExecRequest{
		Path: "/bin/echo",
		Args: []string{"hello", "world"},
		Env:  []string{"LANG=en_US.UTF-8"},
	}

	rp, err := c.Exec(req)

	assert.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)

	defer cancel()

	var got ExecRequest

	select {
	case got = <-rcChan:
	case <-ctx.Done():
		t.Fatal("timeout")
	}

	_, err = io.WriteString(rp.Stdin, "stdin")

	assert.Equal(t, req, got)

	out, err := io.ReadAll(rp.Stdout)

	assert.NoError(t, err)
	assert.Equal(t, "stdout", string(out))

	out, err = io.ReadAll(rp.Stderr)

	assert.NoError(t, err)

	assert.Equal(t, "stderr", string(out))

	rp.SignalSender <- 1

	signal := <-rp.SignalReceiver

	assert.Equal(t, int8(1), signal)
}

type mockStream struct {
	*bytes.Buffer

	closed bool
}

func (m *mockStream) Close() error {
	m.closed = true

	return nil
}
