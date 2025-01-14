package railyard

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/host"
	"github.com/amadigan/macoby/internal/rpc"
	"github.com/containerd/containerd/api/events"
	containerdclient "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/typeurl/v2"
	devents "github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
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

func debugVM(ctx context.Context, cli *Cli) error {
	if err := cli.setup(); err != nil {
		return err
	}

	layout := cli.Config

	control := &host.ControlServer{
		Layout: layout,
		Home:   cli.Home,
		Env:    cli.Env,
	}

	layout.Console = true

	layout.SetDefaults()
	layout.SetDefaultSockets()

	logChan := make(chan applog.Event, 100)
	go debugLog(logChan)

	eventHandler := func(event rpc.LogEvent) {
		logChan <- applog.Event{Subsystem: event.Name, Data: event.Data}
	}

	if err := layout.ResolvePaths(cli.Env, cli.Home); err != nil {
		return fmt.Errorf("failed to resolve paths: %w", err)
	}

	confJson, err := json.MarshalIndent(layout, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal railyard.json: %w", err)
	}

	layout.Console = true

	log.Infof("resolved configuration: %s", string(confJson))

	listeners := make([]net.Listener, 0, len(layout.DockerSocket.HostPath))

	for _, path := range layout.DockerSocket.HostPath {
		network, addr, err := path.ResolveListenSocket(cli.Env, cli.Home)
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

	vm, err := host.NewVirtualMachine(*layout)

	if err != nil {
		return fmt.Errorf("failed to create VM: %w", err)
	}

	start := time.Now()

	if err := vm.Start(eventHandler); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	var dockerdJson []byte

	if len(layout.DockerConfig) > 0 {
		if dockerdJson, err = json.Marshal(layout.DockerConfig); err != nil {
			return fmt.Errorf("failed to marshal dockerd config: %w", err)
		}
	}

	dockerdCmd := rpc.Command{
		Name:  "dockerd",
		Path:  "/usr/bin/dockerd",
		Args:  []string{"dockerd", "--config-file", "/proc/self/fd/0"},
		Input: dockerdJson,
	}

	if err := vm.LaunchService(ctx, dockerdCmd); err != nil {
		return fmt.Errorf("failed to launch dockerd: %w", err)
	}

	log.Infof("dockerd started in %s", time.Since(start))

	network, addr, err := layout.ControlSocket.ResolveListenSocket(cli.Env, cli.Home)
	if err != nil {
		return fmt.Errorf("failed to resolve listen socket %s:%s: %w", network, addr, err)
	}

	listener, err := net.Listen(network, addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s%s: %w", network, addr, err)
	}

	control.SetupServer()

	go control.Serve(listener)

	go monitorContainerd(ctx, vm)
	go monitorDockerd(ctx, vm)

	for _, listener := range listeners {
		go vm.Forward(listener, "unix", layout.DockerSocket.ContainerPath)
	}

	log.Infof("listening on %v", layout.DockerSocket.HostPath)

	<-ctx.Done()

	log.Infof("Shutting down VM")

	if err := vm.Shutdown(); err != nil {
		return fmt.Errorf("failed to shutdown VM: %w", err)
	}

	return nil
}

func debugLog(ch <-chan applog.Event) {
	for event := range ch {
		lines := strings.Split(strings.TrimSpace(string(event.Data)), "\n")

		for _, line := range lines {
			if line = strings.TrimSpace(line); line != "" {
				applog.Log(applog.LogLevelInfo, time.Now(), event.Subsystem, line)
			}
		}
	}
}

func monitorContainerd(ctx context.Context, vm *host.VirtualMachine) {
	log.Infof("monitoring containerd")

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff:           backoff.DefaultConfig,
			MinConnectTimeout: 10 * time.Second,
		}),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return vm.Dial("unix", strings.TrimPrefix(addr, "unix://"))
		}),
	}

	containerd, err := containerdclient.New("/run/docker/containerd/containerd.sock", containerdclient.WithDialOpts(dialOpts))
	if err != nil {
		log.Errorf("failed to connect to containerd: %v", err)

		return
	}

	defer containerd.Close()

	msgChan, errChan := containerd.EventService().Subscribe(ctx)

	go func() {
		for err := range errChan {
			log.Errorf("containerd event error: %v", err)
		}
	}()

	for msg := range msgChan {
		log.Infof("containerd event %T: %v", msg.Event, msg)

		decoded, err := typeurl.UnmarshalAny(msg.Event)
		if err != nil {
			log.Errorf("failed to unmarshal event: %v", err)

			continue
		}

		log.Infof("decoded event %T: %v", decoded, decoded)

		if cev, ok := decoded.(*events.ContainerCreate); ok {
			log.Infof("container create: %s", cev.ID)

			ctxns := namespaces.WithNamespace(ctx, msg.Namespace)

			cont, err := containerd.ContainerService().Get(ctxns, cev.ID)
			if err != nil {
				log.Errorf("failed to get container %s: %v", cev.ID, err)

				continue
			}

			log.Infof("container %s: %+v", cev.ID, cont)
		}
	}
}

func monitorDockerd(ctx context.Context, vm *host.VirtualMachine) {
	log.Infof("monitoring dockerd")

	dclient, err := client.NewClientWithOpts(
		client.WithHost("http://localhost"),
		client.WithDialContext(func(ctx context.Context, network, addr string) (net.Conn, error) {
			return vm.Dial("unix", "/run/docker.sock")
		}),
	)

	if err != nil {
		log.Errorf("failed to connect to dockerd: %v", err)

		return
	}

	defer dclient.Close()

	msgCh, errCh := dclient.Events(ctx, devents.ListOptions{})

	go func() {
		for err := range errCh {
			log.Errorf("dockerd event error: %v", err)
		}
	}()

	for msg := range msgCh {
		typ := msg.Type
		action := msg.Action
		actor := msg.Actor

		if typ == devents.ContainerEventType && action == devents.ActionStart {
			log.Infof("dockerd container start %s: %s %s", action, actor.ID, actor.Attributes)

			cont, err := dclient.ContainerInspect(ctx, actor.ID)
			if err != nil {
				log.Errorf("failed to inspect container %s: %v", actor.ID, err)

				continue
			}

			log.Infof("container %s: %+v", actor.ID, cont)
		}
	}
}
