package host

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/config"
)

type ControlServer struct {
	http.Server
	Layout         *config.Layout
	Home           string
	Env            map[string]string
	Emitter        *applog.Emitter
	Logs           *applog.LogDirectory
	ControlSockets []net.Listener
	vm             *VirtualMachine

	mutex sync.RWMutex
}

func (cs *ControlServer) SetupServer() {
	cs.Handler = cs.newMux()
}

func (c *ControlServer) newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/%s.json", config.Name), c.handleConfig)
	return mux
}

func (c *ControlServer) handleConfig(w http.ResponseWriter, r *http.Request) {
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
