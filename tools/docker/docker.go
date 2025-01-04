package main

import (
	"fmt"

	"github.com/docker/cli/cli/command"
)

func main() {

	cli, err := command.NewDockerCli()

	if err != nil {
		panic(err)
	}

	fmt.Println(cli)
}
