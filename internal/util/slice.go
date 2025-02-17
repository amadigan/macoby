package util

func SliceOf[T any](v ...T) []T {
	return v
}

func MapSlice[I any, O any](f func(I) O, s []I) []O {
	out := make([]O, len(s))

	for i, v := range s {
		out[i] = f(v)
	}

	return out
}
