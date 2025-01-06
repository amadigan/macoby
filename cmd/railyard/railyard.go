package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/amadigan/macoby/cli/railyard"
	"github.com/docker/cli/cli"
	pluginmanager "github.com/docker/cli/cli-plugins/manager"
	"github.com/docker/cli/cli-plugins/socket"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/commands"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func main() {
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

	opts, helpCmd := cli.SetupRootCommand(cmd)

	setupHelpCommand(dockerCli, cmd, helpCmd)

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

	ctx := context.Background()

	if cli.HasCompletionArg(args) {
		// We add plugin command stubs early only for completion. We don't
		// want to add them for normal command execution as it would cause
		// a significant performance hit.
		err = pluginmanager.AddPluginCommandStubs(dockerCli, cmd)
		if err != nil {
			panic(err)
		}
	}

	var subCommand *cobra.Command
	if len(args) > 0 {
		ccmd, _, err := cmd.Find(args)
		subCommand = ccmd
		if err != nil || pluginmanager.IsPluginCommand(ccmd) {
			err := tryPluginRun(ctx, dockerCli, cmd, args[0], os.Environ())
			if err == nil {
				if dockerCli.HooksEnabled() && dockerCli.Out().IsTerminal() && ccmd != nil {
					pluginmanager.RunPluginHooks(ctx, dockerCli, cmd, ccmd, args)
				}
				return
			}
			if !pluginmanager.IsNotFound(err) {
				// For plugin not found we fall through to
				// cmd.Execute() which deals with reporting
				// "command not found" in a consistent way.
				panic(err)
			}
		}
	}

	// This is a fallback for the case where the command does not exit
	// based on context cancellation.
	go forceExitAfter3TerminationSignals(ctx, dockerCli.Err())

	// We've parsed global args already, so reset args to those
	// which remain.
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(ctx)

	// If the command is being executed in an interactive terminal
	// and hook are enabled, run the plugin hooks.
	if dockerCli.HooksEnabled() && dockerCli.Out().IsTerminal() && subCommand != nil {
		var errMessage string
		if err != nil {
			errMessage = err.Error()
		}
		pluginmanager.RunCLICommandHooks(ctx, dockerCli, cmd, subCommand, errMessage)
	}

	if err != nil {
		panic(err)
	}
}

// forceExitAfter3TerminationSignals waits for the first termination signal
// to be caught and the context to be marked as done, then registers a new
// signal handler for subsequent signals. It forces the process to exit
// after 3 SIGTERM/SIGINT signals.
func forceExitAfter3TerminationSignals(ctx context.Context, w io.Writer) {
	// wait for the first signal to be caught and the context to be marked as done
	<-ctx.Done()
	// register a new signal handler for subsequent signals
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	// once we have received a total of 3 signals we force exit the cli
	for i := 0; i < 2; i++ {
		<-sig
	}
	_, _ = fmt.Fprint(w, "\ngot 3 SIGTERM/SIGINTs, forcefully exiting\n")
	os.Exit(1)
}

func setupHelpCommand(dockerCli command.Cli, rootCmd, helpCmd *cobra.Command) {
	origRun := helpCmd.Run
	origRunE := helpCmd.RunE

	helpCmd.Run = nil
	helpCmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) > 0 {
			helpcmd, err := pluginmanager.PluginRunCommand(dockerCli, args[0], rootCmd)
			if err == nil {
				return helpcmd.Run()
			}
			if !pluginmanager.IsNotFound(err) {
				return errors.Errorf("unknown help topic: %v", strings.Join(args, " "))
			}
		}
		if origRunE != nil {
			return origRunE(c, args)
		}
		origRun(c, args)
		return nil
	}
}

func tryPluginRun(ctx context.Context, dockerCli command.Cli, cmd *cobra.Command, subcommand string, envs []string) error {
	plugincmd, err := pluginmanager.PluginRunCommand(dockerCli, subcommand, cmd)
	if err != nil {
		return err
	}

	// Establish the plugin socket, adding it to the environment under a
	// well-known key if successful.
	srv, err := socket.NewPluginServer(nil)
	if err == nil {
		plugincmd.Env = append(plugincmd.Env, socket.EnvKey+"="+srv.Addr().String())
	}
	defer func() {
		// Close the server when plugin execution is over, so that in case
		// it's still open, any sockets on the filesystem are cleaned up.
		_ = srv.Close()
	}()

	// Set additional environment variables specified by the caller.
	plugincmd.Env = append(plugincmd.Env, envs...)

	// Background signal handling logic: block on the signals channel, and
	// notify the plugin via the PluginServer (or signal) as appropriate.
	const exitLimit = 2

	tryTerminatePlugin := func(force bool) {
		// If stdin is a TTY, the kernel will forward
		// signals to the subprocess because the shared
		// pgid makes the TTY a controlling terminal.
		//
		// The plugin should have it's own copy of this
		// termination logic, and exit after 3 retries
		// on it's own.
		if dockerCli.Out().IsTerminal() {
			return
		}

		// Terminate the plugin server, which will
		// close all connections with plugin
		// subprocesses, and signal them to exit.
		//
		// Repeated invocations will result in EINVAL,
		// or EBADF; but that is fine for our purposes.
		_ = srv.Close()

		// force the process to terminate if it hasn't already
		if force {
			_ = plugincmd.Process.Kill()
			_, _ = fmt.Fprint(dockerCli.Err(), "got 3 SIGTERM/SIGINTs, forcefully exiting\n")
			os.Exit(1)
		}
	}

	go func() {
		retries := 0
		force := false
		// catch the first signal through context cancellation
		<-ctx.Done()
		tryTerminatePlugin(force)

		// register subsequent signals
		signals := make(chan os.Signal, exitLimit)
		signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)

		for range signals {
			retries++
			// If we're still running after 3 interruptions
			// (SIGINT/SIGTERM), send a SIGKILL to the plugin as a
			// final attempt to terminate, and exit.
			if retries >= exitLimit {
				force = true
			}
			tryTerminatePlugin(force)
		}
	}()

	if err := plugincmd.Run(); err != nil {
		statusCode := 1
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return err
		}
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			statusCode = ws.ExitStatus()
		}
		return cli.StatusError{
			StatusCode: statusCode,
		}
	}
	return nil
}
