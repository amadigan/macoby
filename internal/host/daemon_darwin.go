package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/controlsock"
	"github.com/amadigan/macoby/internal/event"
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
	layout, _, err := config.LoadConfig(env, "")
	if layout == nil {
		panic(err)
	}

	listener, err := controlsock.ListenSocket(layout.Home)
	if err != nil {
		panic(err)
	}

	control := &ControlServer{
		Layout: layout,
		Home:   layout.Home,
		Env:    env,
	}

	ctx := event.NewBus(context.Background())

	if err := control.SetupLogging(ctx); err != nil {
		panic(err)
	}

	log.Infof("env: %+v", env)

	if err := control.Layout.ResolvePaths(env); err != nil {
		log.Fatal(fmt.Errorf("failed to resolve paths: %w", err))
	}

	futureListener := util.Await(func() (*Listener, error) {
		return NewListener(control.Layout.HostIface)
	})

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

	vm := &VirtualMachine{
		Layout:       *control.Layout,
		LogChannel:   control.LogChannel,
		StateChannel: stateCh,
	}

	dockerJson := util.Await(func() ([]byte, error) {
		if len(control.Layout.DockerConfig) > 0 {
			return json.Marshal(control.Layout.DockerConfig)
		}

		return nil, nil
	})

	control.SetupServer(ctx, vm)

	go func() {
		if err := control.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("failed to serve control: %w", err)
		}
	}()

	defer control.Close()

	start := time.Now()

	if err := control.vm.Start(ctx, state); err != nil {
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

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	svc, err := control.vm.LaunchService(ctx, dockerdCmd)
	if err != nil {
		log.Errorf("failed to launch dockerd: %w", err)

		return
	}

	log.Infof("dockerd started in %s", time.Since(start))

	go MonitorDockerd(ctx, control.vm, futureListener) // forwards container ports to the host

	control.vm.UpdateStatus(ctx, event.StatusReady)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh)

	if layout.IdleTimeout > 0 {
		control.stopLatch = NewStopLatch(layout.IdleTimeout, func() {
			close(sigCh)
		})

		go MonitorContainerd(ctx, control.vm, control.stopLatch)
	}

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

	if err := control.vm.client.GC(struct{}{}, nil); err != nil {
		log.Errorf("failed to GC: %w", err)
	}

	for sig := range sigCh {
		log.Infof("received signal %s", sig)

		if sig == syscall.SIGINT || sig == syscall.SIGTERM {
			break
		}
	}

	stateCh <- DaemonState{Status: StatusStopping}

	log.Info("shutting down")

	if err := svc.Signal(int(syscall.SIGTERM)); err != nil {
		log.Warnf("failed to signal dockerd: %v", err)
	}

	exit := svc.Wait()

	log.Infof("dockerd exited with code %d", exit)
}

func (cs *ControlServer) SetupLogging(ctx context.Context) error {
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

	logChan := make(chan applog.Message, 100)

	files, err := cs.Logs.Open(ctx, logChan)

	if err != nil {
		return fmt.Errorf("failed to open log directory: %w", err)
	}

	cs.logFiles = make(map[string]*util.List[applog.LogFile], len(cs.Logs.Streams)+1)

	for stream, logFile := range files {
		cs.logFiles[stream] = util.NewList(logFile)
	}

	openCh := make(chan event.TypedEnvelope[event.OpenLogFile], 10)
	deleteCh := make(chan event.TypedEnvelope[event.DeleteLogFile], 10)

	go func() {
		for openEv := range openCh {
			cs.addLogFile(openEv.Event.Stream, openEv.Event.Path)
		}
	}()

	go func() {
		for deleteEv := range deleteCh {
			cs.removeLogFile(deleteEv.Event.Stream, deleteEv.Event.Path)
		}
	}()

	cs.LogChannel = logChan

	applog.SetOutput(applog.NewMessageChanWriter("daemon", logChan))

	return nil
}

func (cs *ControlServer) addLogFile(stream string, path string) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	if files, ok := cs.logFiles[stream]; ok {
		files.PushFront(applog.LogFile{Path: path})
	} else {
		cs.logFiles[stream] = util.NewList(applog.LogFile{Path: path})
	}
}

func (cs *ControlServer) removeLogFile(stream string, path string) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	if files, ok := cs.logFiles[stream]; ok {
		for file, node := range files.Cursor() {
			if file.Path == path {
				node.Remove()

				break
			}
		}
	}
}

func (cs *ControlServer) ForwardLaunchdDockerSockets(count int) error {
	for i := range count {
		socks, err := launchd.Sockets(fmt.Sprintf("docker%d", i))
		if err != nil {
			return fmt.Errorf("failed to get docker%d socket: %w", i, err)
		}

		for _, sock := range socks {
			log.Infof("docker listening on %s", sock.Addr())

			go cs.vm.ForwardStopLatch(sock, "unix", cs.Layout.DockerSocket.ContainerPath, cs.stopLatch)
		}
	}

	return nil
}
