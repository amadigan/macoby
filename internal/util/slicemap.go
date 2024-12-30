package util

type SliceMap[K comparable, V any] map[K][]V

func (m SliceMap[K, V]) Put(k K, v V) {
	m[k] = append(m[k], v)
}

func (m SliceMap[K, V]) GetAll(k K) []V {
	return m[k]
}

func (m SliceMap[K, V]) FindFirst(k K, check func(V) bool) (rv V, found bool) {
	for _, v := range m[k] {
		if check(v) {
			return v, true
		}
	}
	return rv, false
}

func (m SliceMap[K, V]) FindAll(k K, check func(V) bool) []V {
	var rv []V
	for _, v := range m[k] {
		if check(v) {
			rv = append(rv, v)
		}
	}
	return rv
}

func (m SliceMap[K, V]) Reduce(k K, start V, reduce func(V, V) V) V {
	for _, v := range m[k] {
		start = reduce(start, v)
	}
	return start
}
