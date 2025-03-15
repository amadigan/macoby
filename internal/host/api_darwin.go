package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/event"
	"github.com/amadigan/macoby/internal/host/config"
	"github.com/amadigan/macoby/internal/util"
	"github.com/coder/websocket"
)

type ControlServer struct {
	http.Server
	Layout         *config.Layout
	Home           string
	Env            map[string]string
	Logs           *applog.LogDirectory
	LogChannel     chan<- applog.Message
	ControlSockets []net.Listener
	stopLatch      *StopLatch
	vm             *VirtualMachine
	logFiles       map[string]*util.List[applog.LogFile]
	mux            *http.ServeMux

	mutex sync.RWMutex
}

func (cs *ControlServer) SetupServer(ctx context.Context, vm *VirtualMachine) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	cs.mux = cs.newMux(ctx)
	cs.Handler = cs
	cs.vm = vm
}

func (cs *ControlServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Infof("new request from %s: %s", r.RemoteAddr, r.URL.Path)
	cs.mux.ServeHTTP(w, r)
}

func (c *ControlServer) newMux(ctx context.Context) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/%s.json", config.Name), func(w http.ResponseWriter, r *http.Request) {
		c.handleConfig(ctx, w, r)
	})

	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		c.handleEvents(ctx, w, r)
	})

	return mux
}

func (c *ControlServer) handleConfig(_ context.Context, w http.ResponseWriter, _ *http.Request) {
	bs, err := json.MarshalIndent(c.Layout, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bs)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bs)
}

func (c *ControlServer) handleEvents(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	log.Infof("new websocket connection from %s", r.RemoteAddr)
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	statusCode := websocket.StatusGoingAway
	statusMsg := "goodbye"

	defer func() {
		_ = conn.Close(statusCode, statusMsg)
	}()

	sync := event.Sync{
		Status:  event.StatusReady, // TODO
		Metrics: c.vm.Metrics(),
		Logs:    make(map[string][]event.LogFile, len(c.logFiles)),
	}

	for stream, files := range c.logFiles {
		sync.Logs[stream] = make([]event.LogFile, 0, files.Len())

		for n := range files.FromFront() {
			sync.Logs[stream] = append(sync.Logs[stream], event.LogFile{Path: n.Path, Offset: n.Offset})
		}
	}

	if err := writeEvent(ctx, conn, sync); err != nil {
		log.Errorf("failed to write initial metrics: %v", err)

		statusCode = websocket.StatusInternalError
		statusMsg = "write failed"

		return
	}

	ch := make(chan event.Envelope, 64)
	event.Tap(ctx, ch)
	defer event.Untap(ctx, ch)

	for e := range ch {
		if err := writeEvent(ctx, conn, e); err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
				log.Errorf("failed to write event: %v", err)

				statusCode = websocket.StatusInternalError
				statusMsg = "write failed"
			}

			break
		}
	}
}

func writeEvent(ctx context.Context, conn *websocket.Conn, e any) error {
	bs, err := json.Marshal(e)
	if err != nil {
		return err
	}

	return conn.Write(ctx, websocket.MessageText, bs) //nolint:wrapcheck
}
