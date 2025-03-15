package host

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/amadigan/macoby/internal/util"
	"github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/api/services/tasks/v1"
	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/typeurl/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
)

type containerTask struct {
	id  string
	pid uint32
}

func MonitorContainerd(ctx context.Context, vm *VirtualMachine, sl *StopLatch) {
	log.Infof("monitoring containerd")

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff:           backoff.DefaultConfig,
			MinConnectTimeout: 10 * time.Second,
		}),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return vm.Dial("unix", strings.TrimPrefix(addr, "unix://"))
		}),
	}

	containerd, err := client.New("/run/docker/containerd/containerd.sock", client.WithDialOpts(dialOpts))
	if err != nil {
		log.Errorf("failed to connect to containerd: %v", err)

		return
	}

	defer containerd.Close()

	msgChan, errChan := containerd.EventService().Subscribe(ctx)

	go func() {
		for err := range errChan {
			log.Errorf("containerd event error: %v", err)
		}
	}()

	var taskSet util.Set[containerTask]

	nss, err := containerd.NamespaceService().List(ctx)
	if err != nil {
		log.Errorf("failed to list namespaces: %v", err)
	} else {
		for _, ns := range nss {
			nsctx := namespaces.WithNamespace(ctx, ns)
			if resp, err := containerd.TaskService().List(nsctx, &tasks.ListTasksRequest{}); err != nil {
				log.Errorf("failed to list tasks in namespace %s: %v", ns, err)
			} else {
				for _, task := range resp.Tasks {
					log.Infof("containerd task %s: %+v", task.ID, task)

					task := containerTask{id: task.ID, pid: task.Pid}

					if !taskSet.Contains(task) {
						taskSet.Add(task)
						sl.Add(1)
					}
				}
			}
		}
	}

	for msg := range msgChan {
		log.Infof("containerd event %T: %v", msg.Event, msg)

		decoded, err := typeurl.UnmarshalAny(msg.Event)
		if err != nil {
			log.Errorf("failed to unmarshal event: %v", err)

			continue
		}

		log.Infof("decoded event %T: %v", decoded, decoded)

		if cev, ok := decoded.(*events.ContainerCreate); ok {
			log.Infof("container create: %s", cev.ID)

			ctxns := namespaces.WithNamespace(ctx, msg.Namespace)

			cont, err := containerd.ContainerService().Get(ctxns, cev.ID)
			if err != nil {
				log.Errorf("failed to get container %s: %v", cev.ID, err)

				continue
			}

			log.Infof("container %s: %+v", cev.ID, cont)
		} else if tev, ok := decoded.(*events.TaskStart); ok {
			log.Infof("task start, container %s, pid %d", tev.ContainerID, tev.Pid)
			taskSet.Add(containerTask{id: tev.ContainerID, pid: tev.Pid})
			sl.Add(1)
		} else if tev, ok := decoded.(*events.TaskDelete); ok {
			log.Infof("task exit, container %s, pid %d, status %d", tev.ContainerID, tev.Pid, tev.ExitStatus)

			if !taskSet.Contains(containerTask{id: tev.ContainerID, pid: tev.Pid}) {
				log.Warnf("unknown task exit, container %s, pid %d, status %d", tev.ContainerID, tev.Pid, tev.ExitStatus)
			} else {
				sl.Add(-1)
				taskSet.Remove(containerTask{id: tev.ContainerID, pid: tev.Pid})
			}

		}
	}
}
