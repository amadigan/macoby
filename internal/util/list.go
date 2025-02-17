package util

import (
	"encoding/json"
	"iter"
)

type List[T any] struct {
	head   *listNode[T]
	tail   *listNode[T]
	length int
}

type listNode[T any] struct {
	value T
	next  *listNode[T]
	prev  *listNode[T]
	list  *List[T]
}

func NewList[T any](items ...T) *List[T] {
	l := &List[T]{}

	for _, v := range items {
		l.PushBack(v)
	}

	return l
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

type ListNode[T any] interface {
	Value() T
	InsertBefore(v T)
	InsertAfter(v T)
	Remove()
}

func (n *listNode[T]) Value() T {
	return n.value
}

func (n *listNode[T]) InsertBefore(v T) {
	newNode := &listNode[T]{value: v, list: n.list, prev: n.prev, next: n}

	if n.prev == nil {
		n.list.head = newNode
	} else {
		n.prev.next = newNode
	}

	n.prev = newNode
	n.list.length++
}

func (n *listNode[T]) InsertAfter(v T) {
	newNode := &listNode[T]{value: v, list: n.list, prev: n, next: n.next}

	if n.next == nil {
		n.list.tail = newNode
	} else {
		n.next.prev = newNode
	}

	n.next = newNode
	n.list.length++
}

func (n *listNode[T]) Remove() {
	if n.prev == nil {
		n.list.head = n.next
	} else {
		n.prev.next = n.next
	}

	if n.next == nil {
		n.list.tail = n.prev
	} else {
		n.next.prev = n.prev
	}

	n.list.length--
}

func (l *List[T]) Cursor() iter.Seq2[T, ListNode[T]] {
	return func(f func(T, ListNode[T]) bool) {
		for n := l.head; n != nil; n = n.next {
			if !f(n.value, n) {
				break
			}
		}
	}
}

func (l *List[T]) Slice() []T {
	s := make([]T, 0, l.length)

	for n := l.head; n != nil; n = n.next {
		s = append(s, n.value)
	}

	return s
}

func (l *List[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.Slice())
}
