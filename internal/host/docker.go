package host

import (
	"context"
	"net"
	"strconv"

	"github.com/amadigan/macoby/internal/util"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
)

func MonitorDockerd(ctx context.Context, vm *VirtualMachine, futureListener func() (*Listener, error)) {
	log.Infof("monitoring dockerd")

	dclient, err := client.NewClientWithOpts(
		client.WithHost("http://localhost"),
		client.WithDialContext(func(ctx context.Context, network, addr string) (net.Conn, error) {
			return vm.Dial("unix", "/run/docker.sock")
		}),
	)

	if err != nil {
		log.Errorf("failed to connect to dockerd: %v", err)

		return
	}

	defer dclient.Close()

	msgCh, errCh := dclient.Events(ctx, events.ListOptions{})

	go func() {
		for err := range errCh {
			log.Errorf("dockerd event error: %v", err)
		}
	}()

	listener, err := futureListener()
	if err != nil {
		log.Errorf("failed to get listener: %v", err)
	}

	defer listener.CloseAll()

	listener.VM = vm

	for msg := range msgCh {
		typ := msg.Type
		action := msg.Action
		actor := msg.Actor

		if typ == events.ContainerEventType && action == events.ActionStart {
			cont, err := dclient.ContainerInspect(ctx, actor.ID)
			if err != nil {
				log.Errorf("failed to inspect container %s: %v", actor.ID, err)

				continue
			}

			if len(cont.NetworkSettings.Ports) > 0 {
				var ip net.IP

				if cont.NetworkSettings.IPAddress != "" {
					ip = net.ParseIP(cont.NetworkSettings.IPAddress)
				} else if cont.NetworkSettings.GlobalIPv6Address != "" {
					ip = net.ParseIP(cont.NetworkSettings.GlobalIPv6Address)
				}

				ports := make(map[GuestPort]util.Set[int], len(cont.NetworkSettings.Ports))

				for guest, hp := range cont.NetworkSettings.Ports {
					if guest.Proto() == "tcp" || guest.Proto() == "tcp6" || guest.Proto() == "tcp4" {
						hostPorts := util.Set[int]{}

						for _, hostPort := range hp {
							port, err := strconv.Atoi(hostPort.HostPort)
							if err != nil {
								log.Errorf("invalid host port %s: %v", hostPort.HostPort, err)

								continue
							}

							hostPorts.Add(port)
						}

						if len(hostPorts) > 0 {
							gp := GuestPort{Proto: ListenerProtoTCP, Port: guest.Int()}
							ports[gp] = hostPorts
						}
					}
				}

				if len(ports) > 0 {
					listener.Forward(actor.ID, ip, ports)
				}
			}
		} else if typ == events.ContainerEventType && (action == events.ActionDie || action == events.ActionStop) {
			listener.Close(actor.ID)
		}
	}
}
