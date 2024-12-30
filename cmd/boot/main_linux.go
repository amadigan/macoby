package main

import (
	"log"
	"syscall"

	"github.com/amadigan/macoby/internal/boot"
)

func main() {
	boot.MountCoreFS()

	var info syscall.Sysinfo_t

	if err := syscall.Sysinfo(&info); err != nil {
		log.Printf("Error getting sysinfo: %v", err)
	} else {
		log.Printf("Uptime: %d, load: %d, %d, %d", info.Uptime, info.Loads[0], info.Loads[1], info.Loads[2])
	}

	if err := boot.Shutdown(); err != nil {
		log.Printf("Shutdown failed! %v", err)
	}
}
