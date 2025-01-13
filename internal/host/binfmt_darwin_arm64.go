package host

import (
	"fmt"

	"github.com/Code-Hex/vz/v3"
)

const amd64Magic = "\\x7fELF\\x02\\x01\\x01\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x02\\x00\\x3e\\x00"

// On arm64, use rosetta to run amd64 binaries, if available.
func (vm *VirtualMachine) configureBinfmts() error {
	if vz.LinuxRosettaDirectoryShareAvailability() == vz.LinuxRosettaAvailabilityInstalled {
		log.Debug("enabling rosetta")

		// set up rosetta
		share, err := vz.NewLinuxRosettaDirectoryShare()
		if err != nil {
			return fmt.Errorf("failed to create rosetta share: %w", err)
		}

		cacheOpt, err := vz.NewLinuxRosettaAbstractSocketCachingOptions("binfmt_misc-rosetta")
		if err != nil {
			return fmt.Errorf("failed to create rosetta caching options: %w", err)
		}

		share.SetOptions(cacheOpt)

		fsconf, err := vz.NewVirtioFileSystemDeviceConfiguration("rosetta")
		if err != nil {
			return fmt.Errorf("failed to create file system device configuration: %w", err)
		}

		fsconf.SetDirectoryShare(share)

		vm.shares = append(vm.shares, fsconf)
		vm.mounts = append(vm.mounts, diskMount{
			mountpoint: "/mnt/rosetta",
			mountFunc: func(vm *VirtualMachine) error {
				log.Info("enabling rosetta")
				if err := vm.Mount("rosetta", "/mnt/rosetta", "virtiofs", []string{"ro"}); err != nil {
					return fmt.Errorf("failed to mount /mnt/rosetta: %w", err)
				}

				return vm.registerBinfmt("rosetta", amd64Magic, "/mnt/rosetta/rosetta")
			},
		})
	}

	return nil
}
