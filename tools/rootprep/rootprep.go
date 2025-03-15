package main

import (
	"log"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		os.Exit(1)
	}

	log.Printf("preparing rootfs %s", os.Args[1])

	if err := compileSysctls(os.Args[1]); err != nil {
		log.Printf("failed to compile sysctls: %v", err)
		os.Exit(1)
	}
}
