package rpc

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"runtime"
	"time"
)

type clockMessage struct {
	Id       uint32
	TimeSec  int64
	TimeNsec int64
}

func HostClock(conn net.Conn) {
	defer conn.Close()
	//runtime.LockOSThread()
	//defer runtime.UnlockOSThread()

	var cm clockMessage

	for {
		if err := binary.Read(conn, binary.LittleEndian, &cm.Id); err != nil {
			if err != io.EOF {
				log.Errorf("clock server read failed: %v", err)
			}

			return
		}

		now := time.Now()
		cm.TimeSec = now.Unix()
		cm.TimeNsec = int64(now.Nanosecond())

		if err := binary.Write(conn, binary.LittleEndian, &cm); err != nil {
			log.Errorf("clock server write failed: %v", err)
			return
		}
	}
}

func GuestClock(ctx context.Context, conn net.Conn, interval time.Duration, adjtime func(time.Duration) error) {
	defer conn.Close()

	log.Infof("clock client started on interval %v", interval)

	maxLatency := 5 * time.Millisecond

	for id := uint32(0); ; id++ {
		log.Debugf("clock client sync %d", id)

		var err error
		if maxLatency, err = syncClock(conn, id, adjtime, maxLatency); err != nil {
			log.Errorf("clock client failed: %v", err)

			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func syncClock(conn net.Conn, id uint32, adjtime func(time.Duration) error, maxlatency time.Duration) (time.Duration, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	start := time.Now()

	if err := binary.Write(conn, binary.LittleEndian, id); err != nil {
		if err != io.EOF {
			return maxlatency, fmt.Errorf("clock client write failed: %v", err)
		}

		return maxlatency, nil
	}

	var cm clockMessage
	if err := binary.Read(conn, binary.LittleEndian, &cm); err != nil {
		if err != io.EOF {
			return maxlatency, fmt.Errorf("clock client read failed: %v", err)
		}

		return maxlatency, nil
	}

	stop := time.Now()
	latency := stop.Sub(start) / 2
	if latency > maxlatency {
		log.Warnf("clock client sync took too long: %v", latency)

		return latency * 2, nil
	}

	if cm.Id != id {
		return latency * 2, fmt.Errorf("clock client got unexpected id: %d, expected %d", cm.Id, id)
	}

	serverTime := time.Unix(cm.TimeSec, cm.TimeNsec)
	offset := serverTime.Sub(start.Add(latency))

	if offset.Abs() > time.Millisecond/2 {
		log.Infof("adjusting clock by %v - latency %v", offset, latency)

		if err := adjtime(offset); err != nil {
			return latency * 2, fmt.Errorf("clock adjustment failed: %v", err)
		}
	} else {
		log.Debugf("clock is in sync (%v) - latency %v", offset, latency)
	}

	return latency * 2, nil
}
