package util

import "math"

func Least[T uint64 | int64 | uint32 | int32 | uint16 | int16 | uint8 | int8 | int | uint | float32 | float64](first T, rest ...T) T {
	least := first
	for _, v := range rest {
		if v < least {
			least = v
		}
	}

	return least
}

func Uint64(v int64) uint64 {
	if v < 0 {
		return 0
	}

	return uint64(v)
}

func Int64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}

	return int64(v)
}
