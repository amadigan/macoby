package event

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/amadigan/macoby/internal/util"
	"github.com/fatih/camelcase"
	"github.com/google/uuid"
)

type Envelope struct {
	Event any
	ID    uuid.UUID
	Time  time.Time
}

func (e Envelope) Type() reflect.Type {
	return reflect.TypeOf(e.Event)
}

func (e Envelope) TypeName() string {
	return typeName(e.Type())
}

func (e Envelope) MarshalJSON() ([]byte, error) {
	jsonForm := map[string]any{
		"type":  e.TypeName(),
		"id":    e.ID,
		"time":  e.Time.UTC().Truncate(time.Millisecond),
		"event": e.Event,
	}

	return json.Marshal(jsonForm)
}

var eventTypes = map[string]reflect.Type{}

func RegisterEventType(e any) {
	name := typeName(reflect.TypeOf(e))
	eventTypes[name] = reflect.TypeOf(e)
}

type UnknownEventError struct {
	Type string
}

func (e UnknownEventError) Error() string {
	return fmt.Sprintf("unknown event type: %s", e.Type)
}

func (e *Envelope) UnmarshalJSON(bs []byte) error {
	var jsonForm struct {
		Type  string
		ID    uuid.UUID
		Time  time.Time
		Event json.RawMessage
	}

	if err := json.Unmarshal(bs, &jsonForm); err != nil {
		return err
	}

	typ, ok := eventTypes[jsonForm.Type]
	if !ok {
		return UnknownEventError{Type: jsonForm.Type}
	}

	e.ID = jsonForm.ID
	e.Time = jsonForm.Time

	val := reflect.New(typ)
	if err := json.Unmarshal(jsonForm.Event, val.Interface()); err != nil {
		return err
	}

	e.Event = val.Elem().Interface()

	return nil
}

func typeName(t reflect.Type) string {
	name := t.Name()

	// split by camel case
	words := camelcase.Split(name)

	// remove the word "event"
	if len(words) > 1 {
		for i, word := range words {
			word = strings.ToLower(word)
			if word == "event" {
				words = append(words[:i], words[i+1:]...)

				break
			} else {
				words[i] = word
			}
		}
	}

	return strings.Join(words, "-")
}

type TypedEnvelope[T any] struct {
	Event     T
	ID        uuid.UUID
	Time      time.Time
	EventType reflect.Type
}

func (e TypedEnvelope[T]) TypeName() string {
	return typeName(e.EventType)
}

func (e TypedEnvelope[T]) Type() reflect.Type {
	return e.EventType
}

func (e TypedEnvelope[T]) Envelope() Envelope {
	return Envelope{
		Event: e.Event,
		ID:    e.ID,
		Time:  e.Time,
	}
}

type emitter interface {
	emit(context.Context, Envelope)
	close()
}

type listeners[T any] struct {
	typ reflect.Type
	chs util.Set[chan<- TypedEnvelope[T]]
}

type Key any
type ctxBus struct{}

type bus struct {
	taps      map[chan<- Envelope]struct{}
	listeners map[reflect.Type]emitter

	mutex sync.RWMutex
}

func NewBus(ctx context.Context) context.Context {
	nctx := context.WithValue(ctx, ctxBus{}, &bus{
		taps:      make(map[chan<- Envelope]struct{}),
		listeners: make(map[reflect.Type]emitter),
	})

	context.AfterFunc(nctx, func() {
		if bus, ok := nctx.Value(ctxBus{}).(*bus); ok {
			bus.mutex.Lock()
			defer bus.mutex.Unlock()

			for ch := range bus.taps {
				close(ch)
			}

			for _, l := range bus.listeners {
				l.close()
			}
		}
	})

	return nctx
}

func send[T any](ch chan<- T, e T) (open bool) {
	defer func() {
		if recover() != nil {
			open = false
		}
	}()

	select {
	case ch <- e:
		return true
	default:
		return true
	}
}

func (l *listeners[T]) emit(ctx context.Context, e Envelope) {
	te := TypedEnvelope[T]{ //nolint:forcetypeassert
		ID:        e.ID,
		Time:      e.Time,
		EventType: l.typ,
		Event:     e.Event.(T),
	}

	for ch := range l.chs {
		if !send(ch, te) {
			go func() { Unlisten[T](ctx, ch) }()
		}
	}
}

func (l *listeners[T]) close() {
	for ch := range l.chs {
		close(ch)
	}
}

func Emit(ctx context.Context, event any) {
	bus, ok := ctx.Value(ctxBus{}).(*bus)

	if !ok {
		return
	}

	e := Envelope{
		Event: event,
		ID:    uuid.New(),
		Time:  time.Now(),
	}

	bus.mutex.RLock()
	defer bus.mutex.RUnlock()

	for l := range bus.taps {
		if !send(l, e) {
			go func() { Untap(ctx, l) }()
		}
	}

	if l, ok := bus.listeners[e.Type()]; ok {
		l.emit(ctx, e)
	}
}

func Listen[T any](ctx context.Context, ch chan<- TypedEnvelope[T]) {
	bus, ok := ctx.Value(ctxBus{}).(*bus)

	if !ok {
		panic("event bus not found in context")
	}

	bus.mutex.Lock()
	defer bus.mutex.Unlock()

	typ := reflect.TypeOf(ch).Elem().Field(0).Type
	if l, ok := bus.listeners[typ].(*listeners[T]); ok {
		l.chs.Add(ch)
	} else {
		bus.listeners[typ] = &listeners[T]{
			typ: typ,
			chs: util.NewSet(ch),
		}
	}
}

func Tap(ctx context.Context, ch chan<- Envelope) {
	bus, ok := ctx.Value(ctxBus{}).(*bus)
	if !ok {
		panic("event bus not found in context")
	}

	bus.mutex.Lock()
	defer bus.mutex.Unlock()

	bus.taps[ch] = struct{}{}
}

func Untap(ctx context.Context, ch chan<- Envelope) {
	bus, ok := ctx.Value(ctxBus{}).(*bus)
	if !ok {
		panic("event bus not found in context")
	}

	bus.mutex.Lock()
	defer bus.mutex.Unlock()

	delete(bus.taps, ch)
}

func Unlisten[T any](ctx context.Context, ch chan<- TypedEnvelope[T]) {
	bus, ok := ctx.Value(ctxBus{}).(*bus)
	if !ok {
		panic("event bus not found in context")
	}

	bus.mutex.Lock()
	defer bus.mutex.Unlock()

	typ := reflect.TypeOf(ch).Elem().Field(0).Type
	if l, ok := bus.listeners[typ].(*listeners[T]); ok {
		l.chs.Remove(ch)

		if len(l.chs) == 0 {
			delete(bus.listeners, typ)
		}
	}
}
