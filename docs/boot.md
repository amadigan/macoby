# Boxpark Guest boot process

The Boxpark guest is a minimal Linux distribution that runs in Virtualization.framework on macOS. The init process is a
Go program which performs minimal initialization before handing control to the host via the Boxpark Guest API.


## 1. Host VM configuration

The host VM is configured with:
 - block devices representing the root filesystem and any additional disks
 - virtiofs mounts for shared directories from the host
 - a kernel from the host filesystem
 - the kernel command line (optional)

### Kernel command line

The kernel command line always contains, at minimum, root=/dev/vda and rootfstype=squashfs. The root device is always /dev/vda.

The kernel uses the following default command line:

```root=/dev/vda rootfstype=squashfs console=ttynull```

To enable the console, the host should override this to ```root=/dev/vda rootfstype=squashfs console=hvc0```

## 2. Guest boot process

The guest init is called by the kernel after it has mounted the root and devtmpfs filesystems. The init process performs the following steps:

- Overlay a tmpfs over the root filesystem
- Mount /dev, /proc, /sys, and cgroup2
- Begin syncing the clock with /dev/rtc0
- Bring the network up
- Start the Boxpark Guest API
- Connect to the host event stream

Once the host receives the connection to the event stream, it knows the guest is now ready to receive commands over the API.
The host then takes control, sending commands to the guest.

## 3. Host-Guest initialization

The host now begins applying its configuration to the guest. 

- If a disk resize is pending, the host will execute commands with the RPC API to complete the resize, this may involve a reboot.
- The host will mount any swap devices
- The host will mount any filesystems, in order by path length

## 4. Service Execution

Before starting the service, the host application may mutate the docker daemon configuration in memory, before writing
it to the guest. The host will begin listening on the SystemD notification socket for service status updates. The host then uses a launch connect call to start the docker daemon. Logs are written in batches of lines back to the host.

When the host is notified that docker is ready, it will begin forwarding connections to the docker daemon via the Boxpark Guest Proxy.
