package railyard

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/controlsock"
	"github.com/amadigan/macoby/internal/event"
	"github.com/amadigan/macoby/internal/host"
	"github.com/amadigan/macoby/internal/rpc"
	"github.com/amadigan/macoby/internal/util"
	"github.com/spf13/cobra"
)

func NewDebugCommand(cli *Cli) *cobra.Command {
	cmd := &cobra.Command{
		Hidden: true,
		Use:    "debug",
		Short:  "Start VM in debug mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			return debugVM(cmd.Context(), cli)
		},
	}

	return cmd
}

func debugVM(octx context.Context, cli *Cli) error { //nolint:cyclop,funlen
	if err := cli.setup(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(octx)
	defer cancel()

	ctx = event.NewBus(ctx)

	layout := cli.Config

	control := &host.ControlServer{
		Layout: layout,
		Home:   cli.Config.Home,
		Env:    cli.Env,
	}

	layout.Console = true

	layout.SetDefaults()
	layout.SetDefaultSockets()

	logChan := make(chan applog.Message, 100)
	go debugLog(logChan)

	if err := layout.ResolvePaths(cli.Env); err != nil {
		return fmt.Errorf("failed to resolve paths: %w", err)
	}

	confJson, err := json.MarshalIndent(layout, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal railyard.json: %w", err)
	}

	futureListener := util.Await(func() (*host.Listener, error) {
		return host.NewListener(layout.HostIface)
	})

	layout.Console = true

	log.Infof("resolved configuration: %s", string(confJson))

	listeners := make([]net.Listener, 0, len(layout.DockerSocket.HostPath))

	for _, path := range layout.DockerSocket.HostPath {
		network, addr, err := path.ResolveListenSocket(cli.Env, cli.Config.Home)
		if err != nil {
			return fmt.Errorf("failed to resolve listen socket %s:%s: %w", network, addr, err)
		}

		if network == "unix" {
			os.Remove(addr)
		}

		listener, err := net.Listen(network, addr)
		if err != nil {
			return fmt.Errorf("failed to listen on %s%s: %w", network, addr, err)
		}

		defer listener.Close()

		listeners = append(listeners, listener)
	}

	stateCh := make(chan host.DaemonState, 10)

	vmstate, done, err := host.OpenDaemonState(layout.StateFile.Resolved, host.StatusStarting, stateCh)
	if err != nil {
		close(stateCh)

		return fmt.Errorf("failed to open daemon state: %w", err)
	}

	defer func() {
		stateCh <- host.DaemonState{Status: host.StatusStopped}
		close(stateCh)
		<-done
	}()

	vm := &host.VirtualMachine{
		Layout:       *layout,
		LogChannel:   logChan,
		StateChannel: stateCh,
	}

	start := time.Now()

	if err := vm.Start(ctx, vmstate); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	var dockerdJson []byte

	if len(layout.DockerConfig) > 0 {
		if dockerdJson, err = json.Marshal(layout.DockerConfig); err != nil {
			return fmt.Errorf("failed to marshal dockerd config: %w", err)
		}
	}

	log.Infof("starting dockerd with config: %s", string(dockerdJson))

	dockerdCmd := rpc.Command{
		Name:  "dockerd",
		Path:  "/usr/bin/dockerd",
		Args:  []string{"dockerd", "--config-file", "/proc/self/fd/0"},
		Input: dockerdJson,
	}

	svc, err := vm.LaunchService(ctx, dockerdCmd)
	if err != nil {
		return fmt.Errorf("failed to launch dockerd: %w", err)
	}

	log.Infof("dockerd started in %s", time.Since(start))
	if err := vm.GC(); err != nil {
		log.Warnf("failed to garbage collect: %v", err)
	}

	vm.UpdateStatus(ctx, event.StatusReady)

	listener, err := controlsock.ListenSocket(cli.Config.Home)
	if err != nil {
		return fmt.Errorf("failed to listen on control socket: %w", err)
	}
	defer listener.Close()

	control.SetupServer(ctx, vm)

	go func() {
		_ = control.Serve(listener)
	}()

	sl := host.NewStopLatch(30*time.Second, func() {
		log.Infof("dockerd is inactive, shutting down VM")
		cancel()
	})

	go host.MonitorContainerd(ctx, vm, sl)
	go host.MonitorDockerd(ctx, vm, futureListener)

	for _, listener := range listeners {
		go vm.ForwardStopLatch(listener, "unix", layout.DockerSocket.ContainerPath, sl)
	}

	log.Infof("listening on %v", layout.DockerSocket.HostPath)

	<-ctx.Done()

	stateCh <- host.DaemonState{Status: host.StatusStopping}

	if err := svc.Signal(int(syscall.SIGTERM)); err != nil {
		log.Warnf("failed to signal dockerd: %v", err)
	}

	exit := svc.Wait()

	log.Infof("dockerd exited with code %d", exit)
	log.Infof("Shutting down VM")

	if err := vm.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown VM: %w", err)
	}

	log.Infof("VM shutdown")

	return nil
}

func debugLog(ch <-chan applog.Message) {
	for event := range ch {
		for line := range strings.SplitSeq(strings.TrimSpace(string(event.Data)), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				applog.Log(applog.LogLevelInfo, time.Now(), event.Subsystem, line)
			}
		}
	}
}
