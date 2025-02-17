package railyard

import (
	"context"
	"fmt"
	"strings"

	"github.com/amadigan/macoby/internal/client"
	"github.com/amadigan/macoby/internal/event"
	"github.com/spf13/cobra"
)

func NewStatsCommand(cli *Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			return stats(cmd.Context(), cli)
		},
	}

	return cmd
}

func stats(ctx context.Context, cli *Cli) error {
	if err := cli.setup(); err != nil {
		return err
	}

	eventCh := make(chan event.Envelope, 100)

	sync, err := client.ReceiveEvents(ctx, cli.Home, eventCh)
	if err != nil {
		// TODO implement offline stat single-shot
		panic(err)
	}

	printMetrics(sync.Metrics)

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-eventCh:
			if !ok {
				return nil
			}

			if metrics, ok := ev.Event.(event.Metrics); ok {
				printMetrics(metrics)
			} else {
				log.Warnf("unexpected event: %T %+v", ev.Event, ev.Event)
			}
		}
	}
}

func printMetrics(metrics event.Metrics) {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("uptime: %d, load: %d %d %d, mem free: %d / %d", metrics.Uptime, metrics.Loads[0],
		metrics.Loads[1], metrics.Loads[2], metrics.MemFree, metrics.Mem))

	for label, disk := range metrics.Disks {
		buf.WriteString(fmt.Sprintf(", %s: %d / %d", label, disk.Free, disk.Total))
	}

	log.Info(buf.String())
}
