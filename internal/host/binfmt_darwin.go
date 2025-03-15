package host

import "fmt"

const elfMask = "\\xff\\xff\\xff\\xff\\xff\\xff\\xff\\x00\\xff\\xff\\xff\\xff\\xff\\xff\\xff\\xff\\xfe\\xff\\xff\\xff"

/*
binfmt_misc is used to enable support for docker images from other architectures.

On arm64, rosetta is used for amd64 binaries.
On arm64, qemu is used for: arm(32), ppc64le, s390x, mips64le, riscv64, and i386.

On amd64, qemu is used for: arm64, arm32, ppc64le, s390x, mips64le, and riscv64.
(amd64 and i386 are already supported by the host.)
*/

type binfmt struct {
	name        string
	magic       string
	interpreter string
}

var coreBinfmts = []binfmt{
	{"qemu-arm", "\\x7fELF\\x01\\x01\\x01\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x02\\x00\\x28\\x00", "/usr/bin/qemu-arm"},
	{"qemu-ppc64le", "\\x7fELF\\x02\\x01\\x01\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x02\\x00\\x15\\x00", "/usr/bin/qemu-ppc64le"},
	{"qemu-s390x", "\\x7fELF\\x02\\x01\\x01\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x02\\x00\\x16\\x00", "/usr/bin/qemu-s390x"},
	{"qemu-mips64el", "\\x7fELF\\x02\\x01\\x01\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x02\\x00\\x08\\x00", "/usr/bin/qemu-mips64el"},
}

func (vm *VirtualMachine) registerBinfmts(bfs []binfmt) error {
	for _, bf := range bfs {
		contents := fmt.Sprintf(":%s:M::%s:%s:%s:PCF", bf.name, bf.magic, elfMask, bf.interpreter)

		if err := vm.Write("/proc/sys/fs/binfmt_misc/register", []byte(contents)); err != nil {
			return fmt.Errorf("failed to register binfmt %s: %w", bf.name, err)
		}
	}

	return nil
}
