package host

const arm64Magic = "\\x7fELF\\x02\\x01\\x01\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x02\\x00\\xb7\\x00"

// On amd64, use qemu to run arm64 binaries.
func (vm *VirtualMachine) configureBinfmts() error {
	log.Debug("enabling qemu-aarch64")
	vm.inits = append(vm.inits, func(vm *VirtualMachine) error {
		return vm.registerBinfmt("qemu-aarch64", arm64Magic, "/usr/bin/qemu-aarch64")
	})

	return nil
}
