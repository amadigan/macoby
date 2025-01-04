package vzapi

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockHandler struct {
	InfoFunc     func(context.Context, InfoRequest) InfoResponse
	ShutdownFunc func(context.Context)
	MountFunc    func(context.Context, MountRequest) error
	WriteFunc    func(context.Context, WriteRequest) error
	ConnectFunc  func(context.Context, ConnectRequest) (net.Conn, error)
	LaunchFunc   func(context.Context, LaunchRequest) error
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

func (m *mockHandler) Connect(ctx context.Context, req ConnectRequest) (net.Conn, error) {
	if m.ConnectFunc != nil {
		return m.ConnectFunc(ctx, req)
	}

	return nil, nil
}

func (m *mockHandler) Launch(ctx context.Context, req LaunchRequest) error {
	if m.LaunchFunc != nil {
		return m.LaunchFunc(ctx, req)
	}

	return nil
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

	res, err := c.Info(context.Background(), req)

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

	if err := c.Shutdown(context.Background()); err != nil {
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

	if err := c.Mount(context.Background(), req); err != nil {
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

	if err := c.Write(context.Background(), req); err != nil {
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
func TestConnect(t *testing.T) {
	t.Parallel()

	rcChan := make(chan ConnectRequest, 1)

	defer close(rcChan)

	server := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "GET", r.Method)
			assert.Equal(t, "/", r.URL.Path)
			w.Header().Set("Content-Type", "text/plain")

			w.WriteHeader(http.StatusOK)

			w.Write([]byte("hello, world"))
		}),
	}

	listener := &mockListener{connCh: make(chan net.Conn, 1)}

	go server.Serve(listener)

	defer server.Close()

	handler := &mockHandler{
		ConnectFunc: func(_ context.Context, req ConnectRequest) (net.Conn, error) {
			rcChan <- req
			left, right := net.Pipe()
			listener.connCh <- right

			return left, nil
		},
	}

	c := setup(handler)

	httpClient := http.Client{Transport: &http.Transport{DialContext: c.DialContext}}

	resp, err := httpClient.Get("http://localhost/")
	assert.NoError(t, err)

	bs, err := io.ReadAll(resp.Body)

	assert.NoError(t, err)

	assert.Equal(t, "hello, world", string(bs))
	assert.NoError(t, resp.Body.Close())
	assert.Equal(t, "text/plain", resp.Header.Get("Content-Type"))

	req := <-rcChan

	assert.Equal(t, ConnectRequest{Network: "tcp", Address: "localhost:80"}, req)
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
