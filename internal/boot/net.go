package boot

import (
	"context"
	"fmt"

	"github.com/amadigan/macoby/internal/netconf"
)

func InitializeNetwork() error {
	if err := netconf.EnableLoopback(); err != nil {
		return fmt.Errorf("Failed to bring up loopback: %v", err)
	}

	ifaces, err := netconf.FindConfigurableInterfaces()

	if err != nil {
		return fmt.Errorf("Unable to fetch configurable interfaces: %v", err)
	} else if len(ifaces) > 0 {
		err = netconf.ConfigureDevice(context.Background(), &ifaces[0])

		if err != nil {
			return fmt.Errorf("DHCP error: %v", err)
		}
	} else {
		return fmt.Errorf("No configurable interfaces found")
	}

	return nil
}
