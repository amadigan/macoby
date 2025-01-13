package host

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/config"
	"github.com/amadigan/macoby/internal/rpc"
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

	defer control.Close()

	if err := control.SetupLogging(); err != nil {
		panic(err)
	}

	if err != nil {
		log.Fatal(err)
	}

	if err := control.Layout.ResolvePaths(env, home); err != nil {
		log.Fatal(fmt.Errorf("failed to resolve paths: %w", err))
	}

	if err := control.ListenLaunchdControl(); err != nil {
		log.Fatal(err)
	}

	if control.vm, err = NewVirtualMachine(*control.Layout); err != nil {
		log.Fatal(fmt.Errorf("failed to create VM: %w", err))
	}

	start := time.Now()

	if err := control.vm.Start(control.handleLogEvent); err != nil {
		log.Fatal(fmt.Errorf("failed to start VM: %w", err))
	}

	dockerdCmd := rpc.Command{Name: "dockerd", Path: "/usr/bin/dockerd"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := control.vm.LaunchService(ctx, dockerdCmd); err != nil {
		log.Fatal(fmt.Errorf("failed to launch dockerd: %w", err))
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
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh

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
		return err
	}

	cs.ControlSockets = socks

	for _, sock := range socks {
		log.Infof("control listening on %s", sock.Addr())
		go cs.Serve(sock)
	}

	return nil
}

func (cs *ControlServer) ForwardLaunchdDockerSockets(count int) error {
	for i := 0; i < count; i++ {
		socks, err := launchd.Sockets(fmt.Sprintf("docker%d", i))
		if err != nil {
			return err
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
