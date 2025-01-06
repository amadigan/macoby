package guest

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func InitializeNetwork() (net.IP, net.IP, error) {
	if err := EnableLoopback(); err != nil {
		return nil, nil, fmt.Errorf("Failed to bring up loopback: %v", err)
	}

	ifaces, err := FindConfigurableInterfaces()

	if err != nil {
		return nil, nil, fmt.Errorf("Unable to fetch configurable interfaces: %v", err)
	}

	if len(ifaces) > 0 {
		v4, v6, err := ConfigureDevice(context.Background(), &ifaces[0])

		if err != nil {
			return nil, nil, fmt.Errorf("DHCP error: %v", err)
		}

		return v4, v6, nil
	}

	return nil, nil, fmt.Errorf("No configurable interfaces found")
}

func SetAddress(iface *net.Interface, addr net.IPNet) error {
	link, err := netlink.LinkByIndex(iface.Index)

	if err != nil {
		return err
	}

	err = netlink.AddrReplace(link, &netlink.Addr{IPNet: &addr})

	if err != nil {
		return err
	}

	if link.Attrs().OperState == netlink.OperDown {
		return netlink.LinkSetUp(link)
	}

	return nil
}

func FindConfigurableInterfaces() ([]net.Interface, error) {
	links, err := netlink.LinkList()

	if err != nil {
		return nil, err
	}

	rv := make([]net.Interface, 0, len(links))

	for _, link := range links {
		attrs := link.Attrs()
		if attrs.OperState == netlink.OperDown && attrs.RawFlags&unix.IFF_LOOPBACK == 0 {
			iface, err := net.InterfaceByIndex(attrs.Index)

			if err != nil {
				return nil, err
			}

			rv = append(rv, *iface)
		}
	}

	return rv, nil
}

func ConfigureDevice(ctx context.Context, iface *net.Interface) (net.IP, net.IP, error) {
	link, err := netlink.LinkByIndex(iface.Index)

	if err != nil {
		return nil, nil, err
	}

	if link.Attrs().OperState == netlink.OperDown {
		err = netlink.LinkSetUp(link)

		if err != nil {
			return nil, nil, err
		}
	}

	client, err := nclient4.New(iface.Name)

	if err != nil {
		return nil, nil, err
	}

	lease, err := client.Request(ctx)

	if err != nil {
		return nil, nil, err
	}

	addr := net.IPNet{
		IP:   lease.Offer.YourIPAddr,
		Mask: lease.Offer.SubnetMask(),
	}

	log.Infof("Setting address %s on %s", addr.String(), iface.Name)

	if err = SetAddress(iface, addr); err != nil {
		return nil, nil, fmt.Errorf("Error setting address: %v", err)
	}

	gateway := lease.Offer.GatewayIPAddr

	if gateway.IsUnspecified() {
		gateway = lease.Offer.ServerIPAddr
	}

	err = netlink.RouteReplace(&netlink.Route{
		Src:       addr.IP,
		Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
		Gw:        gateway,
		LinkIndex: link.Attrs().Index,
	})

	if err != nil {
		return nil, nil, fmt.Errorf("Error setting route: %v", err)
	}

	resolvConf := ""
	resolvers := lease.Offer.DNS()

	for _, resolver := range resolvers {
		resolvConf = resolvConf + fmt.Sprintf("nameserver %s\n", resolver.String())
	}

	return addr.IP, nil, os.WriteFile("/etc/resolv.conf", []byte(resolvConf), 0644)
}

func EnableLoopback() error {
	lo, err := netlink.LinkByName("lo")

	if err != nil {
		return fmt.Errorf("Failed to fetch loopback device: %v", err)
	}

	return netlink.LinkSetUp(lo)
}
