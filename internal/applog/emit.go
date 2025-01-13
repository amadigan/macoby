package applog

import (
	"io"
)

type Registration func(*Event)

type Emitter struct {
	receivers []Registration
	ch        chan Event
}

type Event struct {
	Subsystem string
	Data      []byte
}

func NewEmitter(bufsize int, receivers ...Registration) *Emitter {
	emitter := &Emitter{receivers: receivers, ch: make(chan Event, bufsize)}

	go func() {
		for e := range emitter.ch {
			for _, r := range emitter.receivers {
				r(&e)
			}
		}

		for _, r := range emitter.receivers {
			r(nil)
		}
	}()

	return emitter
}

func (e *Emitter) Emit(subsystem string, data []byte) {
	e.ch <- Event{Subsystem: subsystem, Data: data}
}

func (e *Emitter) Close() {
	close(e.ch)
}

func (e *Emitter) Writer(subsystem string) io.Writer {
	return &writer{emitter: e, subsystem: subsystem}
}

type writer struct {
	emitter   *Emitter
	subsystem string
}

func (w *writer) Write(p []byte) (n int, err error) {
	cp := make([]byte, len(p))
	copy(cp, p)
	w.emitter.Emit(w.subsystem, cp)

	return len(p), nil
}

func AllEvents(ch chan<- Event) Registration {
	return func(e *Event) {
		if e == nil {
			close(ch)
		} else {
			ch <- *e
		}
	}
}

func OmitSubsystems(ch chan<- Event, subsystems ...string) Registration {
	switch len(subsystems) {
	case 0:
		return AllEvents(ch)
	case 1:
		return func(e *Event) {
			if e == nil {
				close(ch)
			} else if e.Subsystem != subsystems[0] {
				ch <- *e
			}
		}
	default:
		set := make(map[string]struct{}, len(subsystems))

		for _, s := range subsystems {
			set[s] = struct{}{}
		}

		return func(e *Event) {
			if e == nil {
				close(ch)
			} else if _, ok := set[e.Subsystem]; !ok {
				ch <- *e
			}
		}
	}
}
