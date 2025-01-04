package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Code-Hex/vz/v3"
	"github.com/amadigan/macoby/internal/vzapi"
	"github.com/docker/cli/cli/command"
)

var mountPaths = map[string]string{
	"users": "/Users",
	"vol":   "/Volumes",
}

func main() {
	cli, err := command.NewDockerCli()

	if err != nil {
		panic(err)
	}

	fmt.Println(cli)

	ctx := context.Background()

	kernelCommandLineArguments := []string{
		// Use the first virtio console device as system console.
		"console=hvc0",
		"root=/dev/vda",
		"rootfstype=squashfs",
	}

	vmlinuz := os.Args[1]
	diskPath := os.Args[2]

	var bootLoader *vz.LinuxBootLoader

	bootLoader, err = vz.NewLinuxBootLoader(
		vmlinuz,
		vz.WithCommandLine(strings.Join(kernelCommandLineArguments, " ")),
	)

	if err != nil {
		log.Fatalf("bootloader creation failed: %s", err)
	}
	log.Println("bootLoader:", bootLoader)

	config, err := vz.NewVirtualMachineConfiguration(
		bootLoader,
		uint(runtime.NumCPU()),
		4096*1024*1024,
	)
	if err != nil {
		log.Fatalf("failed to create virtual machine configuration: %s", err)
	}

	// console
	serialPortAttachment, err := vz.NewFileHandleSerialPortAttachment(os.Stdin, os.Stdout)
	if err != nil {
		log.Fatalf("Serial port attachment creation failed: %s", err)
	}
	consoleConfig, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialPortAttachment)
	if err != nil {
		log.Fatalf("Failed to create serial configuration: %s", err)
	}
	config.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{
		consoleConfig,
	})

	// network
	natAttachment, err := vz.NewNATNetworkDeviceAttachment()
	if err != nil {
		log.Fatalf("NAT network device creation failed: %s", err)
	}
	networkConfig, err := vz.NewVirtioNetworkDeviceConfiguration(natAttachment)
	if err != nil {
		log.Fatalf("Creation of the networking configuration failed: %s", err)
	}
	config.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{
		networkConfig,
	})
	mac, err := vz.NewRandomLocallyAdministeredMACAddress()
	if err != nil {
		log.Fatalf("Random MAC address creation failed: %s", err)
	}
	networkConfig.SetMACAddress(mac)

	// entropy
	entropyConfig, err := vz.NewVirtioEntropyDeviceConfiguration()
	if err != nil {
		log.Fatalf("Entropy device creation failed: %s", err)
	}
	config.SetEntropyDevicesVirtualMachineConfiguration([]*vz.VirtioEntropyDeviceConfiguration{
		entropyConfig,
	})

	rootAttachment, err := vz.NewDiskImageStorageDeviceAttachment(
		diskPath,
		true,
	)
	if err != nil {
		log.Fatal(err)
	}

	rootImage, err := vz.NewVirtioBlockDeviceConfiguration(rootAttachment)

	if err != nil {
		log.Fatalf("Block device creation failed: %s", err)
	}

	storages := []vz.StorageDeviceConfiguration{rootImage}

	hasDataImage := false

	if len(os.Args) > 3 {
		dataImagePath := os.Args[3]
		hasDataImage = true

		dataAttachment, err := vz.NewDiskImageStorageDeviceAttachment(dataImagePath, false)

		if err != nil {
			log.Fatal(err)
		}

		dataImage, err := vz.NewVirtioBlockDeviceConfiguration(dataAttachment)

		if err != nil {
			log.Fatalf("Block device creation failed: %s", err)
		}

		storages = append(storages, dataImage)
	}

	config.SetStorageDevicesVirtualMachineConfiguration(storages)

	shares := []vz.DirectorySharingDeviceConfiguration{}

	for tag, mountPath := range mountPaths {
		shareDir, err := vz.NewSharedDirectory(mountPath, false)

		if err != nil {
			log.Fatalf("failed to create shared directory: %s", err)
		}

		share, err := vz.NewSingleDirectoryShare(shareDir)

		if err != nil {
			log.Fatalf("failed to create single directory share: %s", err)
		}

		fsconf, err := vz.NewVirtioFileSystemDeviceConfiguration(tag)

		if err != nil {
			log.Fatalf("failed to create file system device configuration: %s", err)
		}

		fsconf.SetDirectoryShare(share)

		shares = append(shares, fsconf)
	}

	config.SetDirectorySharingDevicesVirtualMachineConfiguration(shares)

	// traditional memory balloon device which allows for managing guest memory. (optional)
	memoryBalloonDevice, err := vz.NewVirtioTraditionalMemoryBalloonDeviceConfiguration()
	if err != nil {
		log.Fatalf("Balloon device creation failed: %s", err)
	}
	config.SetMemoryBalloonDevicesVirtualMachineConfiguration([]vz.MemoryBalloonDeviceConfiguration{
		memoryBalloonDevice,
	})

	// socket device (optional)
	vsockDevice, err := vz.NewVirtioSocketDeviceConfiguration()
	if err != nil {
		log.Fatalf("virtio-vsock device creation failed: %s", err)
	}

	config.SetSocketDevicesVirtualMachineConfiguration([]vz.SocketDeviceConfiguration{
		vsockDevice,
	})

	validated, err := config.Validate()
	if !validated || err != nil {
		log.Fatal("validation failed", err)
	}

	vm, err := vz.NewVirtualMachine(config)
	if err != nil {
		log.Fatalf("Virtual machine creation failed: %s", err)
	}

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGTERM)

	if err := vm.Start(); err != nil {
		log.Fatalf("Start virtual machine is failed: %s", err)
	}

	stateCh := vm.StateChangedNotify()

	for {
		state, ok := <-stateCh

		if !ok {
			log.Fatalf("state channel closed")
		}

		log.Println("state:", state)

		if state == vz.VirtualMachineStateRunning {
			break
		}
	}

	socks := vm.SocketDevices()

	if len(socks) == 0 {
		log.Fatalf("no socket devices found")
	}

	sock := socks[0]

	listener, err := sock.Listen(2)

	if err != nil {
		log.Fatalf("failed to listen on socket: %s", err)
	}

	defer listener.Close()

	guestLogs, err := listener.Accept()

	if err != nil {
		log.Fatalf("failed to accept connection: %s", err)
	}

	log.Printf("accepted connection from %s to %s", guestLogs.RemoteAddr(), guestLogs.LocalAddr())

	go func() {
		defer guestLogs.Close()

	}()

	var conn *vz.VirtioSocketConnection

	for {
		vconn, err := sock.Connect(1)

		if err != nil {
			log.Printf("failed to connect to socket: %s", err)
			time.Sleep(1 * time.Second)
		} else {
			conn = vconn
			break
		}
	}

	client := vzapi.Client{Conn: conn}

	info, err := client.Info(ctx, vzapi.InfoRequest{ProtocolVersion: 1})

	if err != nil {
		log.Fatalf("failed to get info: %s", err)
	}

	log.Printf("info: %v", info)

	for tag, mountPath := range mountPaths {
		if err := client.Mount(ctx, vzapi.MountRequest{FS: "virtiofs", Device: tag, Target: mountPath}); err != nil {
			log.Fatalf("failed to mount: %s", err)
		}
	}

	if hasDataImage {
		log.Println("preparing to start docker")

		if err := client.Mount(ctx, vzapi.MountRequest{FS: "ext4", Device: "/dev/vdb", Target: "/var/lib/docker"}); err != nil {
			log.Fatalf("failed to mount: %s", err)
		}

		err := client.Launch(ctx, vzapi.LaunchRequest{
			Path: "/usr/bin/dockerd",
			Env:  []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
		})

		if err != nil {
			log.Fatalf("failed to start dockerd: %s", err)
		}

		log.Println("dockerd started")

		// create ~/.macoby/run on the host
		home, err := os.UserHomeDir()

		if err != nil {
			log.Printf("failed to get user home dir: %s", err)
		}

		runDir := path.Join(home, ".macoby", "run")

		if err := os.MkdirAll(runDir, 0755); err != nil {
			log.Printf("failed to create run dir: %s", err)
		}

		// listen on docker.sock

		dockerSock := path.Join(runDir, "docker.sock")

		if err := os.Remove(dockerSock); err != nil && !os.IsNotExist(err) {
			log.Printf("failed to remove docker.sock: %s", err)
		}

		l, err := net.Listen("unix", dockerSock)

		if err != nil {
			log.Printf("failed to listen on docker.sock: %s", err)
		}

		defer l.Close()

		for {
			log.Printf("waiting for connection on %s", dockerSock)
			conn, err := l.Accept()

			if err != nil {
				log.Printf("failed to accept connection: %s", err)
				break
			}

			log.Printf("accepted connection from %s to %s", conn.RemoteAddr(), dockerSock)

			go func() {
				defer conn.Close()

				vconn, err := sock.Connect(1)

				defer vconn.Close()

				if err != nil {
					log.Printf("failed to connect to socket: %s", err)
				}

				client := vzapi.Client{Conn: vconn}

				log.Printf("connecting to docker.sock")

				rconn, err := client.DialContext(ctx, "unix", "/var/run/docker.sock")

				if err != nil {
					log.Printf("failed to connect to docker.sock: %s", err)
					return
				}

				log.Printf("connected to docker.sock")

				defer rconn.Close()

				go func() {
					io.Copy(rconn, conn)
				}()

				_, err = io.Copy(conn, rconn)

				if err != nil {
					log.Printf("failed to copy to client: %s", err)
				}
			}()
		}

		signal := <-signalCh

		log.Printf("received signal: %v", signal)
	}

	for {
		state, ok := <-stateCh

		if !ok {
			break
		}

		log.Println("state:", state)

		if state == vz.VirtualMachineStateStopped {
			break
		}
	}
}
