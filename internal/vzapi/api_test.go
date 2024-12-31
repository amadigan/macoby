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
	InfoFunc     func(context.Context, InfoRequest) InfoResponse
	ShutdownFunc func(context.Context)
	MountFunc    func(context.Context, MountRequest) error
	WriteFunc    func(context.Context, WriteRequest) error
	ExecFunc     func(context.Context, ExecRequest) (*Process, error)
	ConnectFunc  func(context.Context, ConnectRequest, net.Conn) (net.Conn, error)
}

func (m *mockHandler) Info(ctx context.Context, req InfoRequest) InfoResponse {
	if m.InfoFunc != nil {
		return m.InfoFunc(ctx, req)
	}

	return InfoResponse{}
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

func TestInfo(t *testing.T) {
	t.Parallel()

	handler := &mockHandler{
		InfoFunc: func(_ context.Context, req InfoRequest) InfoResponse {
			return InfoResponse{
				ProtocolVersion: req.ProtocolVersion,
				IP:              "127.0.0.1",
				IPv6:            "::1",
			}
		},
	}

	c := setup(handler)

	req := InfoRequest{ProtocolVersion: 1}

	res, err := c.Info(req)

	assert.NoError(t, err)
	assert.Equal(t, req.ProtocolVersion, res.ProtocolVersion)
	assert.Equal(t, "127.0.0.1", res.IP)
	assert.Equal(t, "::1", res.IPv6)

}

func TestShutdown(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	rcChan := make(chan ExecRequest, 1)

	defer close(rcChan)

	sigOut := make(chan int8, 1)
	sigIn := make(chan int8, 1)

	go func() {
		sig := <-sigIn

		assert.Equal(t, int8(1), sig)

		sigOut <- sig
	}()

	process := &Process{
		Stdin:          &mockStream{Buffer: &bytes.Buffer{}},
		Stdout:         &mockStream{Buffer: bytes.NewBufferString("stdout")},
		Stderr:         &mockStream{Buffer: bytes.NewBufferString("stderr")},
		SignalReceiver: sigIn,
		SignalSender:   sigOut,
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

	outCh := make(chan []byte)
	errCh := make(chan []byte)

	go func() {
		defer close(outCh)
		bs, err := io.ReadAll(rp.Stdout)

		assert.NoError(t, err)

		outCh <- bs
	}()

	go func() {
		defer close(errCh)
		bs, err := io.ReadAll(rp.Stderr)

		assert.NoError(t, err)

		errCh <- bs
	}()

	out := <-outCh
	assert.Equal(t, "stdout", string(out))

	out = <-errCh
	assert.Equal(t, "stderr", string(out))

	rp.SignalSender <- 1

	signal := <-rp.SignalReceiver

	assert.Equal(t, int8(1), signal)
}

func TestConnect(t *testing.T) {
	t.Parallel()

	rcChan := make(chan ConnectRequest, 1)

	defer close(rcChan)

	handler := &mockHandler{
		ConnectFunc: func(_ context.Context, req ConnectRequest, conn net.Conn) (net.Conn, error) {
			rcChan <- req
			return conn, nil
		},
	}

	c := setup(handler)

	req := ConnectRequest{
		Network: "tcp",
		Address: "127.0.0.1:80",
	}

	conn, err := c.Connect(req)

	assert.NoError(t, err)
}

type mockStream struct {
	*bytes.Buffer

	closed bool
}

func (m *mockStream) Close() error {
	m.closed = true

	return nil
}

type mockListener struct {
	connCh chan net.Conn
}

func (m *mockListener) Accept() (net.Conn, error) {
	conn, ok := <-m.connCh

	if !ok {
		return nil, io.EOF
	}

	return conn, nil
}

func (m *mockListener) Close() error {
	close(m.connCh)

	return nil
}

func (m *mockListener) Addr() net.Addr {
	return pipeAddr{}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "mockserver" }
