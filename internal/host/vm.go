package host

import (
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
	"github.com/amadigan/macoby/internal/disk"
	"github.com/amadigan/macoby/internal/rpc"
)

type VMState int

const (
	VMStateCreating VMState = iota
	VMStateBooting
	VMStateInit
	VMStateReady
	VMStateStopping
	VMStateStopped
)

type guestCommand func(*VirtualMachine) error

type VirtualMachine struct {
	state        VMState
	layout       Layout
	vm           *vz.VirtualMachine
	vsock        *vz.VirtioSocketDevice
	client       rpc.Guest
	inits        []func(*VirtualMachine) error
	initResponse rpc.InitResponse

	mutex sync.RWMutex
}

func NewVirtualMachine(layout Layout) (*VirtualMachine, error) {
	vm := &VirtualMachine{
		state:  VMStateCreating,
		layout: layout,
	}

	return vm, nil
}

func (vm *VirtualMachine) Start() error {
	log.Debug("creating VM config")
	config, err := createVMConfig(vm.layout)

	if err != nil {
		return err
	}

	log.Debug("setting up VM base")
	if err := setupVMBase(config); err != nil {
		return err
	}

	var state VirtualMachineState

	_ = ReadJsonConfig(vm.layout.StateFile, &state)

	log.Debug("setting up VM network")
	if err := setupVMNetwork(config, &state); err != nil {
		return err
	}

	log.Debug("preparing disks")
	disks, storages, err := prepareDisks(vm.layout)
	if err != nil {
		return err
	}

	config.SetStorageDevicesVirtualMachineConfiguration(storages)

	log.Debug("setting up shares")
	shares, shareConfs, err := setupShares(vm.layout.Shares)
	if err != nil {
		return err
	}

	config.SetDirectorySharingDevicesVirtualMachineConfiguration(shareConfs)

	log.Debug("validating config")
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

	log.Debug("waiting for state change")
	if _, ok := <-vm.vm.StateChangedNotify(); !ok {
		return errors.New("state channel closed")
	}

	log.Debug("getting socket devices")
	socks := vm.vm.SocketDevices()

	if len(socks) < 1 {
		return errors.New("no socket devices")
	}

	vm.vsock = socks[0]

	log.Debug("listening on socket")
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

	receiver := rpc.NewReceiver(eventStream, 32)

	go func() {
		log.Debug("listening for guest events")
		for {
			event, ok := <-receiver

			if !ok {
				return
			}

			log.Infof("event %s: %s", event.Name, strings.TrimSpace(string(event.Data)))
		}
	}()

	vconn, err := vm.vsock.Connect(1)
	if err != nil {
		return fmt.Errorf("failed to connect to guest: %w", err)
	}

	log.Debug("initializing guest")
	vm.client = rpc.NewGuestClient(gorpc.NewClient(vconn))

	var result rpc.InitResponse

	log.Debug("sending init request")
	if err := vm.client.Init(rpc.InitRequest{OverlaySize: 8 * 1024 * 1024, Sysctl: vm.layout.Sysctl}, &result); err != nil {
		return fmt.Errorf("failed to initialize guest: %w", err)
	}

	log.Debugf("init response: %v", result)

	allMounts := append(disks, shares...)

	slices.SortFunc(allMounts, func(left, right diskMount) int {
		// shortest path first
		return len(left.mountpoint) - len(right.mountpoint)
	})

	for _, mount := range allMounts {
		log.Debugf("mounting %s", mount.mountpoint)

		if err := mount.mountFunc(vm); err != nil {
			return fmt.Errorf("failed to mount %s: %w", mount.mountpoint, err)
		}
	}

	return nil
}

func createVMConfig(layout Layout) (*vz.VirtualMachineConfiguration, error) {
	cmdline := []string{"console=hvc0", "root=/dev/vda", "rootfstype=squashfs"}

	if !layout.Console {
		cmdline[0] = "console=ttysnull"
	}

	bootLoader, err := vz.NewLinuxBootLoader(layout.Kernel, vz.WithCommandLine(strings.Join(cmdline, " ")))

	if err != nil {
		return nil, fmt.Errorf("bootloader creation failed: %w", err)
	}

	config, err := vz.NewVirtualMachineConfiguration(bootLoader, uint(layout.Cpu), layout.Ram*1024*1024)

	if err != nil {
		return nil, fmt.Errorf("config creation failed: %w", err)
	}

	if layout.Console {
		serialPortAttachment, err := vz.NewFileHandleSerialPortAttachment(os.Stdin, os.Stdout)
		if err != nil {
			return nil, fmt.Errorf("Serial port attachment creation failed: %w", err)
		}

		if consoleConfig, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialPortAttachment); err == nil {
			config.SetSerialPortsVirtualMachineConfiguration(sliceOf(consoleConfig))
		} else {
			return nil, fmt.Errorf("Failed to create serial configuration: %w", err)
		}
	}

	return config, nil
}

func setupVMBase(config *vz.VirtualMachineConfiguration) error {
	if entropyConfig, err := vz.NewVirtioEntropyDeviceConfiguration(); err == nil {
		config.SetEntropyDevicesVirtualMachineConfiguration(sliceOf(entropyConfig))
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

	return nil
}

func setupVMNetwork(config *vz.VirtualMachineConfiguration, state *VirtualMachineState) error {
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

	config.SetNetworkDevicesVirtualMachineConfiguration(sliceOf(networkConfig))

	networkConfig.SetMACAddress(macAddr)

	return nil
}

type diskMount struct {
	mountpoint string
	mountFunc  func(*VirtualMachine) error
}

func newBlockDevice(path string, readOnly bool) (vz.StorageDeviceConfiguration, error) {
	attachment, err := vz.NewDiskImageStorageDeviceAttachment(path, readOnly)

	if err != nil {
		return nil, fmt.Errorf("failed to create disk %s attachment: %w", path, err)
	}

	return vz.NewVirtioBlockDeviceConfiguration(attachment)
}

func prepareDisks(layout Layout) ([]diskMount, []vz.StorageDeviceConfiguration, error) {
	var mounts []diskMount
	var storages []vz.StorageDeviceConfiguration

	rootImage, err := newBlockDevice(layout.Root, true)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create root disk %s configuration: %w", layout.Root, err)
	}

	storages = append(storages, rootImage)

	for _, label := range sortKeys(layout.Disks) {
		disk := layout.Disks[label]
		if disk.Mount == "" {
			continue
		}

		size, err := ParseSize(disk.Size)

		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse disk %s size: %s: %w", label, disk.Size, err)
		}

		stat, err := os.Stat(disk.Path)

		if errors.Is(err, os.ErrNotExist) {
			if err := setFileSize(disk.Path, size); err != nil {
				return nil, nil, fmt.Errorf("failed to create disk %s: %w", label, err)
			}
		} else if err != nil {
			return nil, nil, fmt.Errorf("failed to stat disk %s: %w", label, err)
		} else if stat.Size() < int64(size) {
			if err := setFileSize(disk.Path, size); err != nil {
				return nil, nil, fmt.Errorf("failed to resize disk %s: %w", label, err)
			}
		}

		fsIdentify := fsidentifyAsync(size, disk.Path)

		dev, err := newBlockDevice(disk.Path, disk.ReadOnly)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create disk %s configuration: %w", label, err)
		}

		device := fmt.Sprintf("/dev/vd%c", 'a'+len(storages))
		storages = append(storages, dev)

		dm := diskMount{
			mountpoint: disk.Mount,
			mountFunc: func(vm *VirtualMachine) error {
				result := <-fsIdentify

				if result.err != nil {
					return fmt.Errorf("failed to identify filesystem: %w", result.err)
				}

				if result.fs == nil || string(result.fs.Type) != disk.FS {
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
				} else if size != result.fs.Size {
					log.Infof("resizing filesystem %s to %d bytes", disk.Path, size)
					// skip for now
				}

				if err := vm.Mount(device, disk.Mount, disk.FS, disk.Options); err != nil {
					return fmt.Errorf("failed to mount %s: %w", disk.Mount, err)
				}

				return nil
			},
		}

		mounts = append(mounts, dm)
	}

	return mounts, storages, nil
}

func setFileSize(path string, size uint64) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)

	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", path, err)
	}

	defer file.Close()

	if err := file.Truncate(int64(size)); err != nil {
		return fmt.Errorf("failed to truncate file %s: %w", path, err)
	}

	return nil
}

type fsidentifyResult struct {
	fs  *disk.Filesystem
	err error
}

func fsidentifyAsync(size uint64, path string) <-chan fsidentifyResult {
	ch := make(chan fsidentifyResult, 1)

	go func() {
		defer close(ch)
		file, err := os.Open(path)

		defer file.Close()

		if err != nil {
			ch <- fsidentifyResult{nil, fmt.Errorf("failed to open file %s: %w", path, err)}
			return
		}

		fs, err := disk.Identify(size, file)

		if err != nil {
			ch <- fsidentifyResult{nil, fmt.Errorf("failed to identify filesystem: %w", err)}
			return
		}

		ch <- fsidentifyResult{fs, nil}
	}()

	return ch
}

func setupShares(shares map[string]*Share) ([]diskMount, []vz.DirectorySharingDeviceConfiguration, error) {
	mounts := make([]diskMount, 0, len(shares))
	confs := make([]vz.DirectorySharingDeviceConfiguration, 0, len(shares))
	labels := make(map[string]bool)

	for dst, share := range shares {
		log.Infof("share %s -> %s", share.Source, dst)
		name := path.Base(dst)

		if name == "" || name == "." || name == ".." || name == "/" {
			return nil, nil, fmt.Errorf("invalid share path: %s", dst)
		}

		counter := 0

		for name = strings.ToLower(name); labels[name]; {
			counter++
			name = fmt.Sprintf("%s-%d", name, counter)
		}

		labels[name] = true

		shareDir, err := vz.NewSharedDirectory(share.Source, share.ReadOnly)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create shared directory %s: %w", share.Source, err)
		}

		dirShare, err := vz.NewSingleDirectoryShare(shareDir)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create single directory share: %w", err)
		}

		fsconf, err := vz.NewVirtioFileSystemDeviceConfiguration(name)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create file system device configuration: %w", err)
		}

		fsconf.SetDirectoryShare(dirShare)

		confs = append(confs, fsconf)

		mounts = append(mounts, diskMount{
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

	return mounts, confs, nil
}
