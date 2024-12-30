package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/amadigan/macoby/internal/block"
	"github.com/amadigan/macoby/internal/boot"
	"github.com/amadigan/macoby/internal/conf"
	"github.com/google/uuid"
	"github.com/mdlayher/vsock"
	"golang.org/x/sys/unix"
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

	go waitShutdownSignal()

	go launchVSockServer()

	go syncClock()

	runService()

	if err := boot.Shutdown(); err != nil {
		log.Printf("Shutdown failed! %v", err)
	}
}

const SIGSHUTDOWN = syscall.Signal(38)

func waitShutdownSignal() {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh)

	for {
		sig := <-signalCh
		fmt.Printf("Received signal: %v\n", sig)
	}
}

func syncClock() {
	f, err := os.OpenFile("/dev/rtc0", os.O_RDWR, 0)

	if err != nil {
		log.Printf("Failed to open /dev/rtc0: %v", err)

		return
	}

	defer f.Close()

	for {
		start := time.Now()
		rt, err := unix.IoctlGetRTCTime(int(f.Fd()))
		now := time.Now()

		if err != nil {
			log.Printf("Failed to get RTC time: %v", err)

			return
		}

		year := int(rt.Year) + 1900
		month := time.Month(rt.Mon + 1)
		day := int(rt.Mday)
		hour := int(rt.Hour)
		minute := int(rt.Min)
		sec := int(rt.Sec)

		// 3) Convert that into a Go time.Time, assuming UTC.
		rtcTime := time.Date(year, month, day, hour, minute, int(sec), 0, time.UTC)

		if rtcTime.Sub(now).Abs() > 2*time.Second {
			tv := unix.NsecToTimeval(now.UnixNano())

			if err := unix.Settimeofday(&tv); err != nil {
				log.Printf("Failed to set time: %v", err)
			} else {
				log.Printf("Time set to %v, was %v", tv, now)
			}
		}

		elapsed := time.Since(start)

		log.Printf("RTC time: %v, elapsed: %v", rtcTime, elapsed)

		time.Sleep(30 * time.Second)
	}

}

func launchVSockServer() {
	conn, err := vsock.Dial(2, 1, nil)

	if err != nil {
		log.Printf("Failed to dial: %v", err)

		return
	}

	defer conn.Close()

	conn.Write([]byte("Hello, world!\n"))

	rdr := bufio.NewReader(conn)

	for {
		line, err := rdr.ReadString('\n')

		if err != nil {
			log.Printf("Failed to read: %v", err)

			return
		}

		fmt.Printf("Received: %s", line)
	}
}

func runService() {
	devices, err := block.ScanDevices()

	if err != nil {
		log.Printf("Failed to scan devices: %v", err)
	} else {
		fmt.Printf("Devices: %v\n", devices)
	}

	cb, err := os.ReadFile("/etc/macoby.json")

	if err != nil {
		log.Printf("Failed to read config: %v", err)
		return
	}

	fmt.Printf("Config: %s\n", cb)

	var vmconf conf.VMConf

	if err := json.Unmarshal(cb, &vmconf); err != nil {
		log.Printf("Failed to unmarshal config: %v", err)

		return
	}

	for mountpoint, conf := range vmconf.Mountpoints {
		table := devices.Devices[conf.Device]

		if table == nil {
			log.Printf("Device %s not found", conf.Device)

			continue
		}

		if err := os.MkdirAll(mountpoint, 0755); err != nil {
			log.Printf("Failed to create mountpoint %s: %v", mountpoint, err)

			continue
		}

		if table.Partitions[0].FilesystemType == "" {
			if conf.Format {
				if err := block.FormatRaw(table.Partitions[0].Device, "", uuid.Nil); err != nil {
					log.Printf("Failed to format %s: %v", table.Partitions[0].Device, err)

					continue
				}
			} else {
				log.Printf("No filesystem on %s", table.Partitions[0].Device)

				continue
			}
		}

		if err := syscall.Mount(table.Partitions[0].Device, mountpoint, conf.FSType, unix.MS_NOATIME, ""); err != nil {
			log.Printf("Failed to mount %s: %v", table.Partitions[0].Device, err)

			continue
		}
	}

	cmd := exec.Command(vmconf.Service)
	cmd.Env = append(os.Environ(), "PATH=/bin:/sbin:/usr/bin:/usr/sbin:/usr/local/bin:/usr/local/sbin")
	cmd.Args = []string{"dockerd"}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		log.Printf("service exited: %v", err)
	}

}
