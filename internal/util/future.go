package util

import "fmt"

type futureResult[T any] struct {
	rv  T
	err error
}

func Await[T any](fn func() (T, error)) func() (T, error) {
	ch := make(chan futureResult[T], 1)

	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				if err, ok := r.(error); ok {
					ch <- futureResult[T]{err: err}
				} else {
					ch <- futureResult[T]{err: fmt.Errorf("panic: %v", r)}
				}
			}
		}()

		rv, err := fn()
		ch <- futureResult[T]{rv, err}
	}()

	var result *futureResult[T]

	return func() (T, error) {
		if result == nil {
			res, ok := <-ch

			if !ok {
				panic("future already resolved")
			}

			result = &res
		}

		return result.rv, result.err
	}
}
