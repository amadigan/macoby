package guest

import (
	"os"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

func StartClockSync(interval time.Duration, stop <-chan struct{}) {
	f, err := os.OpenFile("/dev/rtc0", os.O_RDWR, 0)

	if err != nil {
		log.Errorf("Failed to open /dev/rtc0: %v", err)

		return
	}

	defer f.Close()

	for {
		runtime.LockOSThread()
		rt, err := unix.IoctlGetRTCTime(int(f.Fd()))
		now := time.Now()

		if err != nil {
			log.Errorf("Failed to get RTC time: %v", err)

			return
		}

		year := int(rt.Year) + 1900
		month := time.Month(rt.Mon + 1)
		day := int(rt.Mday)
		hour := int(rt.Hour)
		minute := int(rt.Min)
		sec := int(rt.Sec)

		rtcTime := time.Date(year, month, day, hour, minute, int(sec), 0, time.UTC)
		tv := unix.NsecToTimeval(rtcTime.UnixNano())

		if rtcTime.Sub(now).Abs() >= 2*time.Second {
			if err := unix.Settimeofday(&tv); err != nil {
				log.Errorf("Failed to set time: %v", err)
			} else {
				log.Infof("Time set to %v, was %v", tv, now)
			}
		}
		runtime.UnlockOSThread()

		select {
		case <-time.After(interval):
		case <-stop:
			return
		}
	}

}
