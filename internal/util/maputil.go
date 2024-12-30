package util

func Entries[K comparable, V any](m map[K]V) []V {
	rv := make([]V, 0, len(m))
	for _, v := range m {
		rv = append(rv, v)
	}
	return rv
}
