# railyard daemon API (`railyard.sock`)

The railyard daemon API is an HTTP/1.1 / WebSocket API implemented over a Unix domain socket. The socket is located
at `${RAILYARD_HOME}/run/railyard.sock` (typically ~/Library/Application Support/railyard/run/railyard.sock). 

The daemon API allows the railyard CLI or VS Code extension to control the railyard daemon.

## Connection

When the railyard daemon is running, it listens on the Unix domain socket at `${RAILYARD_HOME}/run/railyard.sock`. If the socket is not present, or does not immediately accept connections, the railyard daemon is not running.

## API

/logs - GET

Returns a JSON object representing the location of the log files for the railyard daemon.

```json
{
  "railyard": [
    {
      "path": "/path/to/railyard.log",
      "offset": 0,
      "start": "2021-01-01T00:00:00Z"
    }
  ]
}
```

/railyard.json - GET

Returns the server's interpretation of the current configuration file, as JSON.

/railyard.yaml - GET

Returns the raw configuration file, which may be the "default" configuration, embedded in the binary.

/update - POST

Triggers the railyard daemon to reload its configuration file.

```json
{
  "purge": [], // list of disks to purge
  "purgeAll": false
}
```
