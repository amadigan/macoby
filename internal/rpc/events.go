package rpc

import (
	"encoding/gob"
	"io"
)

type LogEvent struct {
	Name string
	PID  int64
	Data []byte
}

type EventEmitter struct {
	closer  io.Closer
	encoder *gob.Encoder
	queue   chan LogEvent
}
