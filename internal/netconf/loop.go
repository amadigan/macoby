package netconf

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

func EnableLoopback() error {
	lo, err := netlink.LinkByName("lo")

	if err != nil {
		return fmt.Errorf("Failed to fetch loopback device: %v", err)
	}

	return netlink.LinkSetUp(lo)
}
