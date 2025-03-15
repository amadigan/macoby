package util

import (
	"reflect"
)

func ErrorTypes(err error) []reflect.Type {
	types := NewSet[reflect.Type]()

	errorTypes(err, types)

	return types.Items()
}

type wrappedError interface {
	Unwrap() error
}

type wrappedMultiError interface {
	Unwrap() []error
}

func errorTypes(err error, types Set[reflect.Type]) {
	if err == nil {
		return
	}

	types.Add(reflect.TypeOf(err))

	if wrapped, ok := err.(wrappedError); ok {
		errorTypes(wrapped.Unwrap(), types)
	} else if mw, ok := err.(wrappedMultiError); ok {
		for _, e := range mw.Unwrap() {
			errorTypes(e, types)
		}
	}
}
