package railyard

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/amadigan/macoby/internal/host"
	"github.com/amadigan/macoby/internal/rpc"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type Cli struct {
	Docker *command.DockerCli
}

func NewVMCommand(cli *Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vm",
		Short: "Manage railyard VM",
	}

	cmd.AddCommand(NewDebugCommand(cli))

	return cmd
}

func NewDebugCommand(cli *Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Start VM in debug mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			var dir string

			if len(args) > 0 {
				dir = args[0]
			}

			if err := debugVM(dir); err != nil {
				log.Println(err)
				return err
			}

			return nil
		},
	}

	return cmd
}

func debugVM(dir string) error {
	log.Println("Starting VM in debug mode")

	resolver, err := host.NewFileResolver()
	if err != nil {
		return fmt.Errorf("failed to create file resolver: %w", err)
	}

	if dir != "" {
		dir, err := filepath.Abs(dir)

		if err != nil {
			return fmt.Errorf("failed to resolve %s: %w", dir, err)
		}

		resolver.SetHome(dir)
	}

	confPath := resolver.ResolveInputFile("railyard.jsonc", "railyard.json")
	if confPath == "" {
		return fmt.Errorf("failed to find railyard.jsonc or railyard.json")
	}

	var layout host.Layout
	if err := host.ReadJsonConfig(confPath, &layout); err != nil {
		return fmt.Errorf("failed to read railyard.json: %w", err)
	}

	layout.SetDefaults()

	if err := resolver.ResolvePaths(&layout); err != nil {
		return fmt.Errorf("failed to resolve paths: %w", err)
	}

	confJson, err := json.MarshalIndent(layout, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal railyard.json: %w", err)
	}

	layout.Console = true

	log.Println("Configuration:" + string(confJson))
	fmt.Println(string(confJson))

	os.Remove(layout.DockerSocket.HostPath)

	listener, err := net.Listen("unix", layout.DockerSocket.HostPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", layout.DockerSocket.HostPath, err)
	}

	defer listener.Close()

	vm, err := host.NewVirtualMachine(layout)

	if err != nil {
		return fmt.Errorf("failed to create VM: %w", err)
	}

	if err := vm.Start(); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	for file, content := range layout.JsonConfigs {
		bs, err := json.Marshal(content)

		if err != nil {
			return fmt.Errorf("failed to marshal %s: %w", file, err)
		}

		if err := vm.Write(file, bs); err != nil {
			return fmt.Errorf("failed to write %s: %w", file, err)
		}
	}

	conn, err := vm.ListenUnixgram("unixgram", &net.UnixAddr{Name: "/run/notify.sock", Net: "unixgram"})
	if err != nil {
		return fmt.Errorf("failed to listen on vm:/run/notify.sock: %w", err)
	}

	dockerdCmd := rpc.Command{
		Name: "dockerd",
		Path: "/usr/bin/dockerd",
		Env: []string{
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"NOTIFY_SOCKET=/run/notify.sock",
		},
	}

	if pid, err := vm.Launch(dockerdCmd); err != nil {
		return fmt.Errorf("failed to launch dockerd: %w", err)
	} else {
		log.Printf("dockerd started with pid %d", pid)
	}

	bs := make([]byte, 8192)

	for {
		n, raddr, err := conn.ReadFrom(bs)

		if err != nil {
			return fmt.Errorf("notify: failed to read from socket: %w", err)
		}

		if string(bs[:n]) == "READY=1" {
			log.Printf("notify: received READY from %s", raddr.String())
			break
		} else {
			log.Printf("notify: unexpected message from %s: %s", raddr.String(), string(bs[:n]))
		}
	}

	log.Printf("listening on unix://%s", layout.DockerSocket.HostPath)

	for {
		conn, err := listener.Accept()

		if err != nil {
			return fmt.Errorf("failed to accept connection: %w", err)
		}

		log.Printf("accepted connection from %s", conn.RemoteAddr())

		go func() {
			defer conn.Close()

			vconn, err := vm.Dial("unix", layout.DockerSocket.ContainerPath)
			if err != nil {
				log.Printf("failed to connect to socket: %s", err)
				return
			}

			defer vconn.Close()

			go io.Copy(vconn, conn)
			io.Copy(conn, vconn)

			log.Printf("proxy connection closed")
		}()
	}

	return nil
}
