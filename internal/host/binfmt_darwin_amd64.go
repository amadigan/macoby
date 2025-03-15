package host

const arm64Magic = "\\x7fELF\\x02\\x01\\x01\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x02\\x00\\xb7\\x00"

var arm64Binfmt = binfmt{
	name:        "qemu-aarch64",
	magic:       arm64Magic,
	interpreter: "/usr/bin/qemu-aarch64",
}

// On amd64, use qemu to run arm64 binaries.
func (vm *VirtualMachine) configureBinfmts() error {
	vm.inits = append(vm.inits, func(vm *VirtualMachine) error {
		return vm.registerBinfmts(append(coreBinfmts, arm64Binfmt))
	})

	return nil
}
