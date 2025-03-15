package host

import "time"

type StopLatch struct {
	delay time.Duration
	stop  func()
	ch    chan int64
}

func (s *StopLatch) run() {
	var count uint64

	for {
		select {
		case add, ok := <-s.ch:
			if !ok {
				return
			}

			if add < 0 {
				sub := uint64(-add)
				if sub > count {
					panic("negative count")
				}

				count -= sub
			} else {
				count += uint64(add)
			}

			log.Debugf("stop latch count: %d", count)
		case <-time.After(s.delay):
			if count == 0 {
				s.stop()

				return
			}
		}
	}
}

func (s *StopLatch) Add(add int64) {
	s.ch <- add
}

func (s *StopLatch) Close() {
	close(s.ch)
}

func NewStopLatch(delay time.Duration, stop func()) *StopLatch {
	s := &StopLatch{
		delay: delay,
		stop:  stop,
		ch:    make(chan int64),
	}

	go s.run()

	return s
}
