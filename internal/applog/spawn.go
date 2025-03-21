package applog

func FanOut[T any](listener func() (T, error), handler func(T), logger *Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("panic in fanout: %v", r)
		}
	}()

	for {
		item, err := listener()
		if err != nil {
			logger.Debugf("listener stopped: %v", err)

			return
		}

		go func(item T) {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("panic in handler: %v", r)
				}
			}()

			handler(item)
		}(item)
	}
}
