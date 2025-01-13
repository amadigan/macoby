package rpc

import (
	"io"
	"net"
	"net/rpc"
)

type Guest interface {
	// Init... initialize the guest
	Init(InitRequest, *InitResponse) error
	// Write... overwrite/create a file
	Write(WriteRequest, *struct{}) error
	// Mkdir... create a directory, including parents
	Mkdir(string, *struct{}) error
	// Mount... mount a filesystem
	Mount(MountRequest, *struct{}) error
	// Run... execute a command synchronously
	Run(Command, *CommandOutput) error
	// Launch... execute a command asynchronously, output sent to event stream
	Launch(Command, *int64) error
	// Wait... wait for a service to exit
	Wait(string, *int) error
	// Release... release a service without calling Wait
	Release(string, *struct{}) error
	// Listen... listen on a network address
	Listen(ListenRequest, *struct{}) error
	// Signal... send a signal to a process
	Signal(SignalRequest, *struct{}) error
	// Metrics... get system metrics
	Metrics([]string, *Metrics) error
	// Shutdown... initiate shutdown
	Shutdown(struct{}, *struct{}) error
}

type InitRequest struct {
	OverlaySize uint64
	Sysctl      map[string]string
}

type InitResponse struct {
	IPv4 net.IP
	IPv6 net.IP
}

type SignalRequest struct {
	Service string
	Pid     int64
	Signal  int
}

type WriteRequest struct {
	Path string
	Data []byte
}

type MountRequest struct {
	FS     string
	Device string
	Target string
	Flags  []string
}

type Command struct {
	Name  string // only applies to Launch, identifies the service in the event log
	Path  string
	Dir   string
	Args  []string
	Env   []string
	Input []byte
}

type CommandOutput struct {
	Output []byte
	Exit   int
}

type Metrics struct {
	Uptime   int64
	Loads    [3]uint64
	Mem      uint64
	MemFree  uint64
	Swap     uint64
	SwapFree uint64
	Procs    uint16
	Disks    map[string]DiskMetrics
}

type DiskMetrics struct {
	Total     uint64
	Free      uint64
	MaxFiles  uint64
	FreeFiles uint64
}

type ListenRequest struct {
	Network string
	Address string
}

// The guest API only runs on one connection per VM
func ServeGuestAPI(g Guest, conn io.ReadWriteCloser) error {
	server := rpc.NewServer()

	if err := server.RegisterName("Guest", g); err != nil {
		return err
	}

	server.ServeConn(conn)

	return nil
}

type GuestClient struct {
	*rpc.Client
}

func NewGuestClient(c *rpc.Client) Guest {
	return &GuestClient{c}
}

func (c *GuestClient) Init(req InitRequest, out *InitResponse) error {
	return c.Call("Guest.Init", req, out)
}

func (c *GuestClient) Write(req WriteRequest, _ *struct{}) error {
	return c.Call("Guest.Write", req, nil)
}

func (c *GuestClient) Mkdir(path string, _ *struct{}) error {
	return c.Call("Guest.Mkdir", path, nil)
}

func (c *GuestClient) Mount(req MountRequest, _ *struct{}) error {
	return c.Call("Guest.Mount", req, nil)
}

func (c *GuestClient) Run(req Command, out *CommandOutput) error {
	return c.Call("Guest.Run", req, out)
}

func (c *GuestClient) Launch(req Command, out *int64) error {
	return c.Call("Guest.Launch", req, out)
}

func (c *GuestClient) Wait(req string, out *int) error {
	return c.Call("Guest.Wait", req, out)
}

func (c *GuestClient) Release(req string, _ *struct{}) error {
	return c.Call("Guest.Release", req, nil)
}

func (c *GuestClient) Listen(req ListenRequest, _ *struct{}) error {
	return c.Call("Guest.Listen", req, nil)
}

func (c *GuestClient) Shutdown(_ struct{}, _ *struct{}) error {
	return c.Call("Guest.Shutdown", struct{}{}, nil)
}

func (c *GuestClient) Metrics(req []string, out *Metrics) error {
	return c.Call("Guest.Metrics", req, out)
}

func (c *GuestClient) Signal(req SignalRequest, _ *struct{}) error {
	return c.Call("Guest.Signal", req, nil)
}
