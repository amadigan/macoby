package rpc

import (
	"encoding/gob"
	"fmt"
	"io"
)

type LogMethod int8

const (
	LogStdout LogMethod = iota
	LogStderr
	LogInternal
	LogExit
)

type LogEvent struct {
	Name   string
	Method LogMethod
	Data   []byte
}

func NewEmitter(w io.WriteCloser, bufsize int) chan<- LogEvent {
	enc := gob.NewEncoder(w)
	ch := make(chan LogEvent, bufsize)

	go func() {
		defer w.Close()

		for e := range ch {
			if err := enc.Encode(e); err != nil {
				panic(fmt.Errorf("failed to encode log event: %w", err))
			}
		}
	}()

	return ch
}

func NewReceiver(r io.ReadCloser, bufsize int) <-chan LogEvent {
	dec := gob.NewDecoder(r)
	ch := make(chan LogEvent, bufsize)

	go func() {
		defer r.Close()
		defer close(ch)

		for {
			var e LogEvent

			if err := dec.Decode(&e); err != nil {
				if err == io.EOF {
					return
				}

				panic(fmt.Errorf("failed to decode log event: %w", err))
			}

			ch <- e
		}
	}()

	return ch
}

type emitterWriter struct {
	emitter chan<- LogEvent
	name    string
	method  LogMethod
}

func (ew *emitterWriter) Write(p []byte) (int, error) {
	ew.emitter <- LogEvent{
		Name:   ew.name,
		Method: ew.method,
		Data:   p,
	}

	return len(p), nil
}

func NewEmitterWriter(emitter chan<- LogEvent, name string, method LogMethod) io.Writer {
	return &emitterWriter{
		emitter: emitter,
		name:    name,
		method:  method,
	}
}
