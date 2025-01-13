package main

import (
	"syscall"

	"github.com/amadigan/macoby/internal/guest"
)

// this is /sbin/init for the guest
func main() {
	if err := guest.StartGuest(); err != nil {
		panic(err)
	}

	if err := syscall.Reboot(syscall.LINUX_REBOOT_CMD_POWER_OFF); err != nil {
		panic(err)
	}

}
