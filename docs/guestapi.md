# Boxpark Guest API

The Boxpark guest API is implemented over virtio sockets. The API consists of multiple protocols running over different
ports on both the host and guest.

## Guest

The guest listens on port 1. The first connection is an RPC connection, which is used to send commands to the guest. Subsequent connections are proxy connections.

### RPC API

The RPC API is a request-response, connection-oriented protocol using RPC. The guest exposes the following RPC methods:

- `Mount` - Mount a filesystem.
- `Run` - Run a command.
- `Write` - Overwrite a file with the given data.
- `Mkdir` - Create a directory.
- `Listen` - Listen on a port.
- `Shutdown` - Shutdown the guest.

### Proxy

The proxy enables the host to expose ports on the guest to the outside world. The beginning of each connection contains a gob-encoded `ProxyRequest` struct. The guest then forwards the connection to the port specified in the `ProxyRequest`. The request also contains the local address on the host side, but this information is not used by the guest.

The proxy supports the following protocols:

- TCP
- UDP
- Unix Stream
- Unix Datagram
- "file" - read a file as a stream
- "launch" - launch a process and connect its stdout/stderr as a gob stream

## Host Ports

The host listens on the following ports:

- 1 - Event Stream
- 2 - Proxy

### Event Stream

The event stream is the first connection made between the host and guest, originating from the host once it has finished
booting. The guest sends a stream of gob-encoded events to the host, which the host can use to monitor the guest's state.

The first message in the event stream is an info message containing the guest's version and IP address. Subsequent messages are log messages.

### Proxy

The host proxy allows the host to listen on addresses within the guest. The host listens on port 2 for incoming connections. The beginning of each connection contains a gob-encoded `ConnectionRequest` struct. The connection request
contains the local and remote address of the connection, including the original requested listen address on the guest side. The host may then process the connection as it sees fit.

New sockets are configured by calling the Listen RPC method on the guest. For datagram protocols, the buffer size must 
be specified in the `Listen` request.

The proxy supports the following protocols:

- TCP
- UDP
- Unix Stream
- Unix Datagram
