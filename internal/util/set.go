package util

import "iter"

type Set[T comparable] map[T]struct{}

func NewSet[T comparable](items ...T) Set[T] {
	if len(items) == 0 {
		return make(Set[T])
	}

	s := make(Set[T], len(items))

	for _, item := range items {
		s.Add(item)
	}

	return s
}

func (s Set[T]) Add(item T) {
	s[item] = struct{}{}
}

func (s Set[T]) AddAll(items ...T) {
	for _, item := range items {
		s.Add(item)
	}
}

func (s Set[T]) Remove(item T) {
	delete(s, item)
}

func (s Set[T]) Contains(item T) bool {
	_, ok := s[item]

	return ok
}

func (s Set[T]) All() iter.Seq[T] {
	return func(yield func(T) bool) {
		for item := range s {
			if !yield(item) {
				break
			}
		}
	}
}

func (s Set[T]) Items() []T {
	return MapKeys(s)
}
