package host

import (
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

	"github.com/Code-Hex/vz/v3"
	"github.com/amadigan/macoby/internal/host/config"
	"github.com/amadigan/macoby/internal/host/disk"
	"github.com/amadigan/macoby/internal/rpc"
	"github.com/amadigan/macoby/internal/util"
)

type guestCommand func(*VirtualMachine) error

type VirtualMachine struct {
	Layout       config.Layout
	StateChannel chan<- DaemonState
	LogHandler   func(rpc.LogEvent)

	vm           *vz.VirtualMachine
	vsock        *vz.VirtioSocketDevice
	rpcConn      net.Conn
	client       rpc.Guest
	mounts       []diskMount
	shares       []vz.DirectorySharingDeviceConfiguration
	storages     []vz.StorageDeviceConfiguration
	inits        []guestCommand
	initResponse rpc.InitResponse
	listeners    map[net.Listener]struct{}

	mutex sync.RWMutex
}

func (vm *VirtualMachine) Start(state DaemonState) error {
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

	vst, ok := <-vm.vm.StateChangedNotify()

	if !ok {
		return errors.New("state channel closed")
	}

	log.Debugf("VM state: %s", vst)

	go func() {
		for state := range vm.vm.StateChangedNotify() {
			log.Debugf("VM state: %s", state)
		}
	}()

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
			vm.LogHandler(event)
		}
	}()

	vconn, err := vm.vsock.Connect(1)
	if err != nil {
		return fmt.Errorf("failed to connect to guest: %w", err)
	}

	log.Debug("initializing guest")

	vm.rpcConn = vconn
	vm.client = rpc.NewGuestClient(gorpc.NewClient(vconn))

	var result rpc.InitResponse

	log.Debug("sending init request")

	if err := vm.client.Init(rpc.InitRequest{OverlaySize: 64 * 1024 * 1024, Sysctl: vm.Layout.Sysctl}, &result); err != nil {
		return fmt.Errorf("failed to initialize guest: %w", err)
	}

	log.Debugf("init response: %v", result)

	state.IPv4Address = result.IPv4.String()

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

	vm.StateChannel <- state

	return nil
}

func (vm *VirtualMachine) createVMConfig() (*vz.VirtualMachineConfiguration, error) {
	cmdline := []string{"root=/dev/vda"}

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

func ptr[T any](v T) *T {
	return &v
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

type diskMount struct {
	mountpoint string
	mountFunc  func(*VirtualMachine) error
}

func newBlockDevice(path string, readOnly bool, cache vz.DiskImageCachingMode, sync vz.DiskImageSynchronizationMode) (vz.StorageDeviceConfiguration, error) {
	attachment, err := vz.NewDiskImageStorageDeviceAttachmentWithCacheAndSync(path, readOnly, cache, sync)
	if err != nil {
		return nil, fmt.Errorf("failed to create disk %s attachment: %w", path, err)
	}

	cfg, err := vz.NewVirtioBlockDeviceConfiguration(attachment)
	if err != nil {
		return nil, fmt.Errorf("failed to create disk %s configuration: %w", path, err)
	}

	return cfg, nil
}

func (vm *VirtualMachine) prepareDisks() error {
	log.Debugf("root disk: %s", vm.Layout.Root)

	rootImage, err := newBlockDevice(vm.Layout.Root.Resolved, true, vz.DiskImageCachingModeCached, vz.DiskImageSynchronizationModeNone)
	if err != nil {
		return fmt.Errorf("failed to create root disk %s configuration: %w", vm.Layout.Root, err)
	}

	vm.storages = append(vm.storages, rootImage)

	for _, label := range util.SortKeys(vm.Layout.Disks) {
		disk := vm.Layout.Disks[label]
		if disk.Mount == "" {
			continue
		}

		log.Debugf("disk %s: %s -> %s", label, disk.Path, disk.Mount)

		size, err := config.ParseSize(disk.Size)

		if err != nil {
			return fmt.Errorf("failed to parse disk %s size: %s: %w", label, disk.Size, err)
		}

		stat, err := os.Stat(disk.Path.Resolved)

		if errors.Is(err, os.ErrNotExist) || (err == nil && stat.Size() < size) {
			if err := setFileSize(disk.Path.Resolved, size); err != nil {
				return fmt.Errorf("failed to create disk %s (%s): %w", label, disk.Path.Resolved, err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to stat disk %s: %w", label, err)
		}

		fsIdentify := fsidentifyAsync(size, disk.Path.Resolved)

		dev, err := newBlockDevice(disk.Path.Resolved, disk.ReadOnly, vz.DiskImageCachingModeCached, vz.DiskImageSynchronizationModeFsync)
		if err != nil {
			return fmt.Errorf("failed to create disk %s configuration: %w", label, err)
		}

		device := fmt.Sprintf("/dev/vd%c", 'a'+len(vm.storages))
		vm.storages = append(vm.storages, dev)

		dm := diskMount{
			mountpoint: disk.Mount,
			mountFunc: func(vm *VirtualMachine) error {
				result, err := fsIdentify()

				if err != nil {
					return fmt.Errorf("failed to identify filesystem: %w", err)
				}

				if result == nil || string(result.Type) != disk.FS {
					progname := "mkfs." + disk.FS
					args := append([]string{progname, "-L", label}, disk.FormatOptions...)
					args = append(args, device)

					// mkfs
					cmd := rpc.Command{Path: "/sbin/" + progname, Args: args}

					if out, err := vm.Run(cmd); err != nil {
						return fmt.Errorf("failed to run mkfs: %w", err)
					} else if out.Exit != 0 {
						return fmt.Errorf("mkfs failed: %s", out.Output)
					}
				} else if size != result.Size {
					log.Infof("resizing filesystem %s to %d bytes", disk.Path, size)
					// skip for now
				}

				if err := vm.Mount(device, disk.Mount, disk.FS, disk.Options); err != nil {
					return fmt.Errorf("failed to mount %s: %w", disk.Mount, err)
				}

				return nil
			},
		}

		vm.mounts = append(vm.mounts, dm)
	}

	return nil
}

func setFileSize(path string, size int64) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)

	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", path, err)
	}

	defer file.Close()

	if err := file.Truncate(size); err != nil {
		return fmt.Errorf("failed to truncate file %s: %w", path, err)
	}

	return nil
}

func fsidentifyAsync(size int64, path string) func() (*disk.Filesystem, error) {
	return util.Await(func() (*disk.Filesystem, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %w", path, err)
		}

		defer file.Close()

		fs, err := disk.Identify(size, file)
		if err != nil {
			return nil, fmt.Errorf("failed to identify filesystem: %w", err)
		}

		return fs, nil
	})
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

const elfMask = "\\xff\\xff\\xff\\xff\\xff\\xff\\xff\\x00\\xff\\xff\\xff\\xff\\xff\\xff\\xff\\xff\\xfe\\xff\\xff\\xff"

func (vm *VirtualMachine) registerBinfmt(name string, magic string, interpreter string) error {
	contents := fmt.Sprintf(":%s:M::%s:%s:%s:PCF", name, magic, elfMask, interpreter)

	return vm.Write("/proc/sys/fs/binfmt_misc/register", []byte(contents))
}
