package main

import (
	"fmt"
	"reflect"

	"github.com/amadigan/macoby/internal/event"
)

type TestEvent struct {
	Message string
}

func main() {
	listener := make(chan event.TypedEnvelope[TestEvent])
	fmt.Println(reflect.TypeOf(listener).Elem().Field(0).Type)
}
