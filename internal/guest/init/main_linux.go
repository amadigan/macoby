package main

import (
	"context"
	"log"
	"time"

	"github.com/amadigan/macoby/internal/guest"
	"github.com/mdlayher/vsock"
)

// this is /bin/init for the guest
func main() {
	must(guest.OverlayRoot(16 * 1024 * 1024))
	must(guest.MountProc())
	must(guest.MountSys())
	must(guest.MountCgroup())
	must(guest.InitializeNetwork())

	go guest.StartClockSync(10*time.Second, make(chan struct{}))

	g := guest.Guest{}

	log.Printf("Starting guest API")

	go func() {
		err := g.Start(context.Background())

		if err != nil {
			log.Fatalf("Failed to start guest API: %v", err)
		}
	}()

	conn, err := vsock.Dial(2, 2, nil)

	if err != nil {
		log.Fatalf("Failed to connect to host: %v", err)
	}

	defer conn.Close()

	log.Printf("Connected to host from %s to %s", conn.LocalAddr(), conn.RemoteAddr())

	// wait for the host to close the connection
	<-make(chan struct{})

	b := make([]byte, 1)

	for {
		_, err := conn.Read(b)

		if err != nil {
			break
		}
	}

	/*
	   cmd := exec.Command("/bin/busybox")

	   cmd.Stdin = os.Stdin
	   cmd.Stdout = os.Stdout
	   cmd.Stderr = os.Stderr

	   cmd.Args = []string{"ash"}

	   	if err := cmd.Run(); err != nil {
	   		log.Fatalf("Failed to run shell: %v", err)
	   	}
	*/
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
