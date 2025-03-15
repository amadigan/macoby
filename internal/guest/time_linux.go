package guest

import (
	"context"
	"fmt"
	"time"

	"github.com/amadigan/macoby/internal/rpc"
	"github.com/mdlayher/vsock"
	"golang.org/x/sys/unix"
)

func StartClockSync(ctx context.Context, interval time.Duration) error {
	// connect to the host clock server running on vsock port 2
	conn, err := vsock.Dial(2, 2, nil)
	if err != nil {
		return fmt.Errorf("failed to dial host clock: %w", err)
	}

	go func() {
		t := &timesync{first: true}
		rpc.GuestClock(ctx, conn, interval, t.adjtime)
	}()

	return nil
}

type timesync struct {
	first bool
}

func (t *timesync) adjtime(offset time.Duration) error {
	var delta unix.Timex
	delta.Status = unix.STA_PLL

	if !t.first && offset.Abs() < 500*time.Millisecond {
		delta.Modes = unix.ADJ_OFFSET | unix.ADJ_NANO | unix.ADJ_STATUS
		delta.Offset = offset.Nanoseconds()
	} else {
		delta.Modes = unix.ADJ_SETOFFSET | unix.ADJ_NANO | unix.ADJ_STATUS
		delta.Time.Sec = int64(offset.Truncate(time.Second).Seconds())
		delta.Time.Usec = (offset - offset.Truncate(time.Second)).Nanoseconds()

		if delta.Time.Usec < 0 {
			delta.Time.Sec--
			delta.Time.Usec += time.Second.Nanoseconds()
		}
	}

	t.first = false

	if _, err := unix.Adjtimex(&delta); err != nil {
		return fmt.Errorf("adjtimex failed: %v", err)
	}

	return nil
}
