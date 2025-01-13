package applog

import (
	"errors"
	"io"
)

func FanOut[T any](listener func() (T, error), handler func(T), logger *Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("panic in fanout: %v", r)
		}
	}()

	for {
		item, err := listener()

		if err != nil && !errors.Is(err, io.EOF) {
			logger.Errorf("error listening for items: %v", err)

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
