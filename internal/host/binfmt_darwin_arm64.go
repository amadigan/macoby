package host

import (
	"fmt"

	"github.com/Code-Hex/vz/v3"
)

const amd64Magic = "\\x7fELF\\x02\\x01\\x01\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x02\\x00\\x3e\\x00"

var rosettaBinfmt = binfmt{
	name:        "rosetta",
	magic:       amd64Magic,
	interpreter: "/mnt/rosetta/rosetta",
}

var qemuAmd64Binfmt = binfmt{
	name:        "qemu-x86_64",
	magic:       amd64Magic,
	interpreter: "/usr/bin/qemu-x86_64",
}

var qemuI386Binfmt = binfmt{
	name:        "qemu-i386",
	magic:       "\\x7fELF\\x01\\x01\\x01\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x02\\x00\\x03\\x00",
	interpreter: "/usr/bin/qemu-i386",
}

// On arm64, use rosetta to run amd64 binaries, if available.
func (vm *VirtualMachine) configureBinfmts() error {
	useRosetta := false

	if rosettaFlag := vm.Layout.Rosetta; rosettaFlag == nil {
		useRosetta = vz.LinuxRosettaDirectoryShareAvailability() == vz.LinuxRosettaAvailabilityInstalled
		vm.Layout.Rosetta = &useRosetta
	} else {
		useRosetta = *rosettaFlag
	}

	amd64binfmt := qemuAmd64Binfmt

	if useRosetta {
		log.Debug("enabling rosetta")

		amd64binfmt = rosettaBinfmt

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
				return nil
			},
		})
	}

	vm.inits = append(vm.inits, func(vm *VirtualMachine) error {
		return vm.registerBinfmts(append(coreBinfmts, amd64binfmt, qemuI386Binfmt))
	})

	return nil
}
