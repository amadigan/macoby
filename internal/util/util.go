package util

import (
	"math"
)

func Align[T int64 | uint64 | int](size, boundary T) T {
	return CountBlocks(size, boundary) * boundary
}

func CountBlocks[T int64 | uint64 | int](size, boundary T) T {
	blocks := float64(size) / float64(boundary)
	return T(math.Ceil(blocks))
}

func Contains[T comparable](slice []T, value T) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

func Must(err error) {
	if err != nil {
		panic(err)
	}
}
