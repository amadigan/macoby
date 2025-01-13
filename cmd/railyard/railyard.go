package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/amadigan/macoby/cli/railyard"
	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/host"
	"github.com/amadigan/macoby/internal/util"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/commands"
	"github.com/spf13/cobra"
)

var log = applog.New("railyard-cli")

func main() {
	if host.IsDaemon(os.Args) {
		host.RunDaemon(os.Args, util.Env())

		return
	}

	dockerCli, err := command.NewDockerCli()
	if err != nil {
		panic(err)
	}

	cmd := &cobra.Command{
		Use:              "railyard [OPTIONS] COMMAND [ARG...]",
		Short:            "Run and manage docker containers on macOS",
		SilenceUsage:     true,
		SilenceErrors:    true,
		TraverseChildren: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return command.ShowHelp(os.Stderr)(cmd, args)
			}

			return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
		},
		DisableFlagsInUseLine: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd:   false,
			HiddenDefaultCmd:    true,
			DisableDescriptions: true,
		},
	}

	cmd.SetIn(os.Stdin)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	opts, _ := cli.SetupRootCommand(cmd)

	commands.AddCommands(cmd, dockerCli)
	cmd.AddCommand(railyard.NewVMCommand(&railyard.Cli{Docker: dockerCli}))

	tcmd := cli.NewTopLevelCommand(cmd, dockerCli, opts, cmd.Flags())

	cmd, args, err := tcmd.HandleGlobalFlags()
	if err != nil {
		panic(err)
	}

	if err := tcmd.Initialize(); err != nil {
		panic(err)
	}

	ctx := createContext()

	if len(args) > 0 {
		if _, _, err := cmd.Find(args); err != nil {
			// command not found
			panic(err)
		}
	}

	// We've parsed global args already, so reset args to those which remain.
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(ctx)

	if err != nil {
		//nolint:forbidigo
		fmt.Println(err)
		os.Exit(1)
	}
}

func createContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	sig := make(chan os.Signal, 3)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sig
		cancel()

		for i := 0; i < 2; i++ {
			<-sig
		}

		log.Infof("got 3 SIGTERM/SIGINTs, forcefully exiting")

		os.Exit(1)
	}()

	return ctx
}
