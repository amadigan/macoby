package main

import (
	"github.com/amadigan/macoby/internal/guest"
)

// this is /sbin/init for the guest
func main() {
	if err := guest.StartGuest(); err != nil {
		panic(err)
	}
}
