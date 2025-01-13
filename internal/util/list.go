package util

import "iter"

type List[T any] struct {
	head   *listNode[T]
	tail   *listNode[T]
	length int
}

type listNode[T any] struct {
	value T
	next  *listNode[T]
	prev  *listNode[T]
}

func (l *List[T]) PushFront(v T) {
	n := &listNode[T]{value: v}

	if l.head == nil {
		l.head = n
		l.tail = n
	} else {
		n.next = l.head
		l.head.prev = n
		l.head = n
	}

	l.length++
}

func (l *List[T]) PushBack(v T) {
	n := &listNode[T]{value: v}

	if l.tail == nil {
		l.head = n
		l.tail = n
	} else {
		n.prev = l.tail
		l.tail.next = n
		l.tail = n
	}

	l.length++
}

func (l *List[T]) PopFront() (value T, ok bool) {
	if l.head == nil {
		return
	}

	value = l.head.value
	l.head = l.head.next
	l.length--

	if l.head == nil {
		l.tail = nil
	} else {
		l.head.prev = nil
	}

	return value, true
}

func (l *List[T]) PopBack() (value T, ok bool) {
	if l.tail == nil {
		return
	}

	value = l.tail.value
	l.tail = l.tail.prev
	l.length--

	if l.tail == nil {
		l.head = nil
	} else {
		l.tail.next = nil
	}

	return value, true
}

func (l *List[T]) Front() (value T, ok bool) {
	if l.head == nil {
		return
	}

	return l.head.value, true
}

func (l *List[T]) Back() (value T, ok bool) {
	if l.tail == nil {
		return
	}

	return l.tail.value, true
}

func (l *List[T]) Len() int {
	return l.length
}

func (l *List[T]) Empty() bool {
	return l.length == 0
}

func (l *List[T]) Clear() {
	l.head = nil
	l.tail = nil
	l.length = 0
}

func (l *List[T]) FromFront() iter.Seq[T] {
	return func(f func(T) bool) {
		for n := l.head; n != nil; n = n.next {
			if !f(n.value) {
				break
			}
		}
	}
}
