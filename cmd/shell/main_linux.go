package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"

	"github.com/amadigan/macoby/internal/block"
	"github.com/amadigan/macoby/internal/boot"
)

func main() {
	if err := boot.OverlayRoot(); err != nil {
		log.Printf("Failed to overlay root: %v", err)
	}

	boot.MountCoreFS()

	log.Print("Initializing network")
	if err := boot.InitializeNetwork(); err != nil {
		log.Printf("Failed to initialize network: %v", err)
	}

	var info syscall.Sysinfo_t

	if err := syscall.Sysinfo(&info); err != nil {
		log.Printf("Error getting sysinfo: %v", err)
	} else {
		log.Printf("Uptime: %d, load: %d, %d, %d", info.Uptime, info.Loads[0], info.Loads[1], info.Loads[2])
	}

	devices, err := block.ScanDevices()

	if err != nil {
		log.Printf("Failed to scan devices: %v", err)
	} else {
		fmt.Printf("Devices: %v\n", devices)
	}

	log.Print("Launching shell")

	cmd := exec.Command("/bin/busybox")
	cmd.Env = append(os.Environ(), "PATH=/bin:/sbin:/usr/bin:/usr/sbin:/usr/local/bin:/usr/local/sbin")
	cmd.Args = []string{"ash"}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Printf("Shell exited: %v", err)
	}

	if err := boot.Shutdown(); err != nil {
		log.Printf("Shutdown failed! %v", err)
	}
}
