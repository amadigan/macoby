package util

import (
	"cmp"
	"slices"
)

func MapKeys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))

	for k := range m {
		keys = append(keys, k)
	}

	return keys
}

func MapValues[K comparable, V any](m map[K]V) []V {
	values := make([]V, 0, len(m))

	for _, v := range m {
		values = append(values, v)
	}

	return values
}

func SortKeys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))

	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}

func MapCopy[K comparable, V any](m map[K]V) map[K]V {
	c := make(map[K]V, len(m))

	for k, v := range m {
		c[k] = v
	}

	return c
}
