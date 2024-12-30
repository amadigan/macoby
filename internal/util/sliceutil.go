package util

func Map[T1, T2 any](in []T1, m func(T1) T2) []T2 {
	out := make([]T2, len(in))

	for i, v := range in {
		out[i] = m(v)
	}

	return out
}
