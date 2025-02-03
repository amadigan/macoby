package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/host/config"
	"github.com/amadigan/macoby/internal/rpc"
	"github.com/amadigan/macoby/internal/util"
	"github.com/bored-engineer/go-launchd"
)

func IsDaemon(osArgs []string) bool {
	if len(osArgs) == 0 {
		return false
	}

	baseName := path.Base(osArgs[0])
	daemonName := config.Name + "d"

	return osArgs[0] == daemonName || osArgs[0] == "daemon" || strings.HasSuffix(baseName, daemonName) || strings.HasPrefix(baseName, daemonName+"-")
}

func RunDaemon(osArgs []string, env map[string]string) {
	layout, home, err := config.LoadConfig(env, "")
	if layout == nil {
		panic(err)
	}

	control := &ControlServer{
		Layout: layout,
		Home:   home,
		Env:    env,
	}

	if err := control.SetupLogging(); err != nil {
		panic(err)
	}

	if err != nil {
		log.Fatal(err)
	}

	log.Infof("env: %+v", env)

	if err := control.Layout.ResolvePaths(env, home); err != nil {
		log.Fatal(fmt.Errorf("failed to resolve paths: %w", err))
	}

	stateCh := make(chan DaemonState, 10)

	state, done, err := OpenDaemonState(control.Layout.StateFile.Resolved, StatusStarting, stateCh)
	if err != nil {
		close(stateCh)
		log.Fatal(fmt.Errorf("failed to open daemon state: %w", err))
	}

	defer func() {
		stateCh <- DaemonState{Status: StatusStopped}
		close(stateCh)
		<-done
	}()

	control.vm = &VirtualMachine{
		Layout:       *control.Layout,
		LogHandler:   control.handleLogEvent,
		StateChannel: stateCh,
	}

	dockerJson := util.Await(func() ([]byte, error) {
		if len(control.Layout.DockerConfig) > 0 {
			return json.Marshal(control.Layout.DockerConfig)
		}

		return nil, nil
	})

	if err := control.ListenLaunchdControl(); err != nil {
		log.Errorf("failed to listen on launchd control: %w", err)

		return
	}

	defer control.Close()

	start := time.Now()

	if err := control.vm.Start(state); err != nil {
		log.Errorf("failed to start VM: %w", err)

		return
	}

	dockerBs, err := dockerJson()
	if err != nil {
		log.Errorf("failed to get docker config: %w", err)

		return
	}

	log.Infof("docker config: %s", string(dockerBs))

	dockerdCmd := rpc.Command{
		Name:  "dockerd",
		Path:  "/usr/bin/dockerd",
		Args:  []string{"dockerd", "--config-file", "/proc/self/fd/0"},
		Input: dockerBs,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := control.vm.LaunchService(ctx, dockerdCmd); err != nil {
		log.Errorf("failed to launch dockerd: %w", err)

		return
	}

	log.Infof("dockerd started in %s", time.Since(start))

	if len(osArgs) > 1 {
		if sockCount, err := strconv.Atoi(osArgs[1]); err == nil {
			if err := control.ForwardLaunchdDockerSockets(sockCount); err != nil {
				log.Errorf("failed to forward docker sockets: %w", err)
			}
		} else {
			log.Errorf("invalid socket count: %s", osArgs[1])
		}
	} else {
		log.Warn("no socket count specified")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh)

	for sig := range sigCh {
		log.Infof("received signal %s", sig)

		if sig == syscall.SIGINT || sig == syscall.SIGTERM {
			break
		}
	}

	log.Info("shutting down")
}

func (cs *ControlServer) SetupLogging() error {
	if err := cs.Layout.Log.Directory.ResolveOutputDir(cs.Env, cs.Home); err != nil {
		return fmt.Errorf("failed to resolve log directory: %w", err)
	}

	cs.Logs = &applog.LogDirectory{
		NameFormat:  "%s-2006-01-02-150405.log",
		MaxFileSize: 5 * 1024 * 1024,
		MaxFiles:    3,
		Fallback:    config.Name,
		Streams:     cs.Layout.Log.Streams,
		Root:        cs.Layout.Log.Directory.Resolved,
	}

	logChan := make(chan applog.Event, 100)

	if ok, err := cs.Logs.Open(logChan); !ok {
		return fmt.Errorf("failed to open log directory: %w", err)
	}

	emitter := applog.NewEmitter(32, applog.AllEvents(logChan))
	cs.Emitter = emitter

	applog.SetOutput(emitter.Writer("daemon"))

	return nil
}

func (cs *ControlServer) ListenLaunchdControl() error {
	cs.SetupServer()

	socks, err := launchd.Sockets("control")
	if err != nil {
		return fmt.Errorf("failed to get control socket: %w", err)
	}

	cs.ControlSockets = socks

	for _, sock := range socks {
		log.Infof("control listening on %s", sock.Addr())

		go func(sock net.Listener) {
			if err := cs.Serve(sock); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Errorf("failed to serve control: %w", err)
			}
		}(sock)
	}

	return nil
}

func (cs *ControlServer) ForwardLaunchdDockerSockets(count int) error {
	for i := 0; i < count; i++ {
		socks, err := launchd.Sockets(fmt.Sprintf("docker%d", i))
		if err != nil {
			return fmt.Errorf("failed to get docker%d socket: %w", i, err)
		}

		for _, sock := range socks {
			log.Infof("docker listening on %s", sock.Addr())

			go cs.vm.Forward(sock, "unix", cs.Layout.DockerSocket.ContainerPath)
		}
	}

	return nil
}

func (cs *ControlServer) handleLogEvent(event rpc.LogEvent) {
	cs.Emitter.Emit(event.Name, event.Data)
}

func (cs *ControlServer) Close() {
	for _, sock := range cs.ControlSockets {
		sock.Close()
	}

	// last thing to close
	if cs.Emitter != nil {
		cs.Emitter.Close()
	}
}
