package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/controlsock"
	"github.com/amadigan/macoby/internal/event"
	"github.com/coder/websocket"
)

var log *applog.Logger = applog.New("client")

func ReceiveEvents(ctx context.Context, home string, ch chan<- event.Envelope) (*event.Sync, error) {
	ws, _, err := websocket.Dial(ctx, "ws://localhost:8080/events", &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return controlsock.DialSocket(home)
				},
			},
		},
	})

	if err != nil {
		return nil, err
	}

	typ, bs, err := ws.Read(ctx)
	if err != nil {
		ws.Close(websocket.StatusInternalError, "failed to read message")

		return nil, err
	}

	if typ != websocket.MessageText {
		ws.Close(websocket.StatusUnsupportedData, "expected text message")

		return nil, err
	}

	var sync event.Sync
	if err := json.Unmarshal(bs, &sync); err != nil {
		ws.Close(websocket.StatusInternalError, "failed to unmarshal message")

		return nil, err
	}

	go func() {
		defer ws.Close(websocket.StatusGoingAway, "goodbye")

		for {
			typ, bs, err := ws.Read(ctx)
			if err != nil {
				break
			}

			if typ != websocket.MessageText {
				break
			}

			var e event.Envelope
			if err := json.Unmarshal(bs, &e); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
					break
				}

				log.Warnf("failed to unmarshal event: %v", err)
			}

			select {
			case ch <- e:
			case <-ctx.Done():
				break
			}
		}

		log.Infof("event stream closed")
	}()

	return &sync, nil
}
