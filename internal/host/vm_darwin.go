package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	gorpc "net/rpc"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Code-Hex/vz/v3"
	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/event"
	"github.com/amadigan/macoby/internal/host/config"
	"github.com/amadigan/macoby/internal/rpc"
	"github.com/amadigan/macoby/internal/util"
)

type guestCommand func(*VirtualMachine) error

type VirtualMachine struct {
	Layout       config.Layout
	StateChannel chan<- DaemonState
	LogChannel   chan<- applog.Message

	status    event.Status
	vm        *vz.VirtualMachine
	vsock     *vz.VirtioSocketDevice
	rpcConn   net.Conn
	client    rpc.Guest
	mounts    []diskMount
	shares    []vz.DirectorySharingDeviceConfiguration
	storages  []vz.StorageDeviceConfiguration
	inits     []guestCommand
	listeners map[net.Listener]struct{}
	metrics   event.Metrics

	ipv4 net.IP

	mutex sync.RWMutex
}

func (vm *VirtualMachine) UpdateStatus(ctx context.Context, status event.Status) {
	vm.mutex.Lock()
	defer vm.mutex.Unlock()

	if vm.status != status {
		vm.status = status
		log.Infof("new vm status: %s", status)
		event.Emit(ctx, status)
	}
}

func (vm *VirtualMachine) Start(ctx context.Context, state DaemonState) error {
	vm.UpdateStatus(ctx, event.StatusBooting)
	vm.listeners = make(map[net.Listener]struct{})
	configs := prepareConfigsAsync(vm.Layout)

	log.Debug("creating VM config")

	config, err := vm.createVMConfig()
	if err != nil {
		return err
	}

	log.Debug("setting up VM base")

	if err := setupVMBase(config, &state); err != nil {
		return err
	}

	log.Debug("setting up VM network")

	if err := setupVMNetwork(config, &state); err != nil {
		return err
	}

	log.Debug("preparing disks")

	if err := vm.prepareDisks(); err != nil {
		return fmt.Errorf("failed to prepare disks: %w", err)
	}

	log.Debug("setting up shares")

	if err := vm.setupShares(); err != nil {
		return err
	}

	if err := vm.configureBinfmts(); err != nil {
		return err
	}

	config.SetStorageDevicesVirtualMachineConfiguration(vm.storages)
	config.SetDirectorySharingDevicesVirtualMachineConfiguration(vm.shares)

	validated, err := config.Validate()
	if !validated || err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	log.Debug("creating VM")

	vm.vm, err = vz.NewVirtualMachine(config)
	if err != nil {
		return fmt.Errorf("failed to create virtual machine: %w", err)
	}

	log.Debug("starting VM")

	if err := vm.vm.Start(); err != nil {
		return fmt.Errorf("failed to start virtual machine: %w", err)
	}

	if st, ok := <-vm.vm.StateChangedNotify(); !ok {
		return errors.New("state channel closed")
	} else if st != vz.VirtualMachineStateRunning && st != vz.VirtualMachineStateStarting {
		return fmt.Errorf("unexpected state: %s", st)
	}

	go func() {
		for state := range vm.vm.StateChangedNotify() {
			log.Infof("VM state: %d", state)

			switch state { //nolint:exhaustive
			case vz.VirtualMachineStateStopped:
				fallthrough
			case vz.VirtualMachineStateError:
				vm.UpdateStatus(ctx, event.StatusStopped)

				return
			case vz.VirtualMachineStateStopping:
				vm.UpdateStatus(ctx, event.StatusStopping)
			}
		}

		log.Debug("VM state channel closed")
	}()

	if err := vm.handshake(); err != nil {
		return err
	}

	log.Debug("sending init request")

	initMsg := rpc.InitRequest{
		OverlaySize:   16 * 1024 * 1024,
		ClockInterval: 10 * time.Second,
		Sysctl:        vm.Layout.Sysctl,
	}

	if err := vm.client.Init(initMsg, nil); err != nil {
		return fmt.Errorf("failed to initialize guest: %w", err)
	}

	dhcp := util.Await(func() (net.IP, error) {
		var result rpc.DHCPResponse

		if err := vm.client.DHCP(struct{}{}, &result); err != nil {
			return nil, fmt.Errorf("failed to get DHCP address: %w", err)
		}

		return result.Address, nil
	})

	confFiles, err := configs()
	if err != nil {
		return fmt.Errorf("failed to prepare configs: %w", err)
	}

	for name, data := range confFiles {
		log.Debugf("sending config %s", name)

		if err := vm.Write(name, data); err != nil {
			return fmt.Errorf("failed to write config %s: %w", name, err)
		}
	}

	if err := vm.mountFilesystems(); err != nil {
		return err
	}

	go vm.metricsLoop(ctx)

	if vm.ipv4, err = dhcp(); err != nil {
		return fmt.Errorf("failed to get DHCP address: %w", err)
	}

	state.IPv4Address = vm.ipv4.String()

	vm.StateChannel <- state

	return nil
}

func (vm *VirtualMachine) handshake() error {
	if socks := vm.vm.SocketDevices(); len(socks) > 0 {
		vm.vsock = socks[0]
	} else {
		return errors.New("no socket devices")
	}

	listener, err := vm.vsock.Listen(1)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}
	defer listener.Close()

	log.Debug("waiting for guest connection")

	eventStream, err := listener.Accept()
	if err != nil {
		return fmt.Errorf("failed to accept connection: %w", err)
	}

	go func() {
		log.Debug("listening for guest events")

		for event := range rpc.NewReceiver(eventStream, 32) {
			vm.LogChannel <- applog.Message{Subsystem: event.Name, Data: event.Data}
		}
	}()

	vconn, err := vm.vsock.Connect(1)
	if err != nil {
		return fmt.Errorf("failed to connect to guest: %w", err)
	}

	log.Debug("initializing guest")

	vm.rpcConn = vconn
	vm.client = rpc.NewGuestClient(gorpc.NewClient(vconn))

	listener, err = vm.vsock.Listen(2)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}

	go func() {
		defer listener.Close()

		conn, err := listener.Accept()

		if err != nil {
			log.Warnf("failed to accept clock connection: %s", err)
			return
		}

		rpc.HostClock(conn)
	}()

	return nil
}

func (vm *VirtualMachine) mountFilesystems() error {
	slices.SortFunc(vm.mounts, func(left, right diskMount) int {
		// shortest path first
		return len(left.mountpoint) - len(right.mountpoint)
	})

	for _, mount := range vm.mounts {
		log.Debugf("mounting %s", mount.mountpoint)

		if err := mount.mountFunc(vm); err != nil {
			return fmt.Errorf("failed to mount %s: %w", mount.mountpoint, err)
		}
	}

	for _, init := range vm.inits {
		if err := init(vm); err != nil {
			return fmt.Errorf("failed to run init: %w", err)
		}
	}

	return nil
}

func (vm *VirtualMachine) metricsLoop(ctx context.Context) {
	interval := time.Duration(vm.Layout.MetricInterval) * time.Second

	vm.mutex.Lock()
	keys := util.MapKeys(vm.metrics.Disks)
	vm.mutex.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			var metrics event.Metrics
			if err := vm.client.Metrics(keys, &metrics); err != nil {
				log.Warnf("failed to get metrics: %s", err)
			} else {
				vm.mutex.Lock()
				vm.metrics = metrics
				vm.mutex.Unlock()

				event.Emit(ctx, metrics)
			}
		}
	}
}

func (vm *VirtualMachine) createVMConfig() (*vz.VirtualMachineConfiguration, error) {
	cmdline := []string{"ro", "root=/dev/vda"}

	if !vm.Layout.Console {
		cmdline = append(cmdline, "quiet", "console=ttysnull")
	} else {
		cmdline = append(cmdline, "console=hvc0")
	}

	bootLoader, err := vz.NewLinuxBootLoader(vm.Layout.Kernel.Resolved, vz.WithCommandLine(strings.Join(cmdline, " ")))

	if err != nil {
		return nil, fmt.Errorf("bootloader creation failed: %w", err)
	}

	config, err := vz.NewVirtualMachineConfiguration(bootLoader, vm.Layout.Cpu, vm.Layout.Ram*1024*1024)

	if err != nil {
		return nil, fmt.Errorf("config creation failed: %w", err)
	}

	if vm.Layout.Console {
		serialPortAttachment, err := vz.NewFileHandleSerialPortAttachment(os.Stdin, os.Stdout)
		if err != nil {
			return nil, fmt.Errorf("Serial port attachment creation failed: %w", err)
		}

		if consoleConfig, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialPortAttachment); err == nil {
			config.SetSerialPortsVirtualMachineConfiguration(util.SliceOf(consoleConfig))
		} else {
			return nil, fmt.Errorf("Failed to create serial configuration: %w", err)
		}
	}

	return config, nil
}

func setupVMBase(config *vz.VirtualMachineConfiguration, state *DaemonState) error {
	if entropyConfig, err := vz.NewVirtioEntropyDeviceConfiguration(); err == nil {
		config.SetEntropyDevicesVirtualMachineConfiguration(util.SliceOf(entropyConfig))
	} else {
		return fmt.Errorf("failed to create entropy device configuration: %w", err)
	}

	if memoryBalloonConfig, err := vz.NewVirtioTraditionalMemoryBalloonDeviceConfiguration(); err == nil {
		config.SetMemoryBalloonDevicesVirtualMachineConfiguration([]vz.MemoryBalloonDeviceConfiguration{memoryBalloonConfig})
	} else {
		return fmt.Errorf("failed to create memory balloon device configuration: %w", err)
	}

	if vsockConfig, err := vz.NewVirtioSocketDeviceConfiguration(); err == nil {
		config.SetSocketDevicesVirtualMachineConfiguration([]vz.SocketDeviceConfiguration{vsockConfig})
	} else {
		return fmt.Errorf("failed to create virtio socket device configuration: %w", err)
	}

	var machineID *vz.GenericMachineIdentifier

	if len(state.MachineID) > 0 {
		var err error
		machineID, err = vz.NewGenericMachineIdentifierWithData(state.MachineID)

		if err != nil {
			log.Warnf("failed to parse machine ID: %s", err)
		}
	}

	if machineID == nil {
		var err error
		machineID, err = vz.NewGenericMachineIdentifier()

		if err != nil {
			log.Warnf("failed to create random machine ID: %s", err)
		} else {
			state.MachineID = machineID.DataRepresentation()
		}
	}

	if machineID != nil {
		platform, err := vz.NewGenericPlatformConfiguration(vz.WithGenericMachineIdentifier(machineID))
		if err != nil {
			return fmt.Errorf("failed to create platform configuration: %w", err)
		}

		if vz.IsNestedVirtualizationSupported() {
			if err := platform.SetNestedVirtualizationEnabled(true); err != nil {
				log.Warnf("failed to enable nested virtualization: %s", err)
			}
		}

		config.SetPlatformVirtualMachineConfiguration(platform)
	}

	return nil
}

func setupVMNetwork(config *vz.VirtualMachineConfiguration, state *DaemonState) error {
	var macAddr *vz.MACAddress

	if state.MACAddress == "" {
		mac, err := vz.NewRandomLocallyAdministeredMACAddress()

		if err != nil {
			return fmt.Errorf("failed to create random MAC address: %w", err)
		}

		state.MACAddress = mac.String()
		macAddr = mac
	} else {
		hwaddr, err := net.ParseMAC(state.MACAddress)

		if err != nil {
			return fmt.Errorf("failed to parse MAC address: %w", err)
		}

		mac, err := vz.NewMACAddress(hwaddr)

		if err != nil {
			return fmt.Errorf("failed to create MAC address: %w", err)
		}

		macAddr = mac
	}

	// network
	natAttachment, err := vz.NewNATNetworkDeviceAttachment()
	if err != nil {
		return fmt.Errorf("NAT network device creation failed: %w", err)
	}

	networkConfig, err := vz.NewVirtioNetworkDeviceConfiguration(natAttachment)
	if err != nil {
		log.Fatalf("Creation of the networking configuration failed: %s", err)
	}

	config.SetNetworkDevicesVirtualMachineConfiguration(util.SliceOf(networkConfig))

	networkConfig.SetMACAddress(macAddr)

	return nil
}

func (vm *VirtualMachine) setupShares() error {
	shares := vm.Layout.Shares
	labels := make(map[string]bool)

	for dst, share := range shares {
		log.Infof("share %s -> %s", share.Source, dst)
		name := path.Base(dst)

		if name == "" || name == "." || name == ".." || name == "/" {
			return fmt.Errorf("invalid share path: %s", dst)
		}

		counter := 0

		for name = strings.ToLower(name); labels[name]; {
			counter++
			name = fmt.Sprintf("%s-%d", name, counter)
		}

		labels[name] = true

		shareDir, err := vz.NewSharedDirectory(share.Source.Resolved, share.ReadOnly)
		if err != nil {
			return fmt.Errorf("failed to create shared directory %s: %w", share.Source, err)
		}

		dirShare, err := vz.NewSingleDirectoryShare(shareDir)
		if err != nil {
			return fmt.Errorf("failed to create single directory share: %w", err)
		}

		fsconf, err := vz.NewVirtioFileSystemDeviceConfiguration(name)
		if err != nil {
			return fmt.Errorf("failed to create file system device configuration: %w", err)
		}

		fsconf.SetDirectoryShare(dirShare)

		vm.shares = append(vm.shares, fsconf)

		vm.mounts = append(vm.mounts, diskMount{
			mountpoint: dst,
			mountFunc: func(vm *VirtualMachine) error {
				var args []string

				if share.ReadOnly {
					args = []string{"ro"}
				}

				if err := vm.Mount(name, dst, "virtiofs", args); err != nil {
					return fmt.Errorf("failed to mount %s: %w", dst, err)
				}

				return nil
			},
		})
	}

	return nil
}

func prepareConfigsAsync(layout config.Layout) func() (map[string][]byte, error) {
	return util.Await(func() (map[string][]byte, error) {
		rv := make(map[string][]byte, len(layout.JsonConfigs))

		for name, data := range layout.JsonConfigs {
			bs, err := json.Marshal(data)

			if err != nil {
				return nil, fmt.Errorf("failed to marshal %s: %w", name, err)
			}

			rv[name] = bs
		}

		return rv, nil
	})
}

func (vm *VirtualMachine) Metrics() event.Metrics {
	vm.mutex.RLock()
	defer vm.mutex.RUnlock()

	return vm.metrics
}
