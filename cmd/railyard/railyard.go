package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/amadigan/macoby/cli/railyard"
	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/host"
	"github.com/amadigan/macoby/internal/util"
	buildxcmd "github.com/docker/buildx/commands"
	"github.com/docker/cli/cli"
	pluginmanager "github.com/docker/cli/cli-plugins/manager"
	"github.com/docker/cli/cli-plugins/socket"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/commands"
	"github.com/docker/docker/api/types/versions"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	_ "github.com/docker/buildx/driver/docker"
	_ "github.com/docker/buildx/driver/docker-container"
	_ "github.com/docker/buildx/driver/kubernetes"
	_ "github.com/docker/buildx/driver/remote"

	// Use custom grpc codec to utilize vtprotobuf
	_ "github.com/moby/buildkit/util/grpcutil/encoding/proto"
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

	opts, helpCmd := cli.SetupRootCommand(cmd)

	setupHelpCommand(dockerCli, cmd, helpCmd)
	setHelpFunc(dockerCli, cmd)

	commands.AddCommands(cmd, dockerCli)
	cmd.AddCommand(railyard.NewVMCommand(&railyard.Cli{Docker: dockerCli}))
	cmd.AddCommand(buildxcmd.NewRootCmd("buildx", false, dockerCli))

	tcmd := cli.NewTopLevelCommand(cmd, dockerCli, opts, cmd.Flags())

	cmd, args, err := tcmd.HandleGlobalFlags()
	if err != nil {
		panic(err)
	}

	if err := tcmd.Initialize(); err != nil {
		panic(err)
	}

	var envs []string
	args, os.Args, envs, err = processBuilder(dockerCli, cmd, args, os.Args)
	if err != nil {
		panic(err)
	}

	if cli.HasCompletionArg(args) {
		// We add plugin command stubs early only for completion. We don't
		// want to add them for normal command execution as it would cause
		// a significant performance hit.
		err = pluginmanager.AddPluginCommandStubs(dockerCli, cmd)
		if err != nil {
			panic(err)
		}
	}

	ctx := createContext()

	if len(args) > 0 {
		ccmd, _, err := cmd.Find(args)
		if err != nil || pluginmanager.IsPluginCommand(ccmd) {
			fmt.Println("plugin command")
			err := tryPluginRun(ctx, dockerCli, cmd, args[0], envs)
			if err == nil {
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
				return fmt.Errorf("unknown help topic: %v", strings.Join(args, " "))
			}
		}
		if origRunE != nil {
			return origRunE(c, args)
		}
		origRun(c, args)
		return nil
	}
}

func setHelpFunc(dockerCli command.Cli, cmd *cobra.Command) {
	defaultHelpFunc := cmd.HelpFunc()
	cmd.SetHelpFunc(func(ccmd *cobra.Command, args []string) {
		if err := pluginmanager.AddPluginCommandStubs(dockerCli, ccmd.Root()); err != nil {
			ccmd.Println(err)
			return
		}

		if len(args) >= 1 {
			err := tryRunPluginHelp(dockerCli, ccmd, args)
			if err == nil {
				return
			}
			if !pluginmanager.IsNotFound(err) {
				ccmd.Println(err)
				return
			}
		}

		if err := isSupported(ccmd, dockerCli); err != nil {
			ccmd.Println(err)
			return
		}
		if err := hideUnsupportedFeatures(ccmd, dockerCli); err != nil {
			ccmd.Println(err)
			return
		}

		defaultHelpFunc(ccmd, args)
	})
}

type versionDetails interface {
	CurrentVersion() string
	ServerInfo() command.ServerInfo
}

func hideFlagIf(f *pflag.Flag, condition func(string) bool, annotation string) {
	if f.Hidden {
		return
	}
	var val string
	if values, ok := f.Annotations[annotation]; ok {
		if len(values) > 0 {
			val = values[0]
		}
		if condition(val) {
			f.Hidden = true
		}
	}
}

func hideSubcommandIf(subcmd *cobra.Command, condition func(string) bool, annotation string) {
	if subcmd.Hidden {
		return
	}
	if v, ok := subcmd.Annotations[annotation]; ok {
		if condition(v) {
			subcmd.Hidden = true
		}
	}
}

func hideUnsupportedFeatures(cmd *cobra.Command, details versionDetails) error {
	var (
		notExperimental = func(_ string) bool { return !details.ServerInfo().HasExperimental }
		notOSType       = func(v string) bool { return details.ServerInfo().OSType != "" && v != details.ServerInfo().OSType }
		notSwarmStatus  = func(v string) bool {
			s := details.ServerInfo().SwarmStatus
			if s == nil {
				// engine did not return swarm status header
				return false
			}
			switch v {
			case "manager":
				// requires the node to be a manager
				return !s.ControlAvailable
			case "active":
				// requires swarm to be active on the node (e.g. for swarm leave)
				// only hide the command if we're sure the node is "inactive"
				// for any other status, assume the "leave" command can still
				// be used.
				return s.NodeState == "inactive"
			case "":
				// some swarm commands, such as "swarm init" and "swarm join"
				// are swarm-related, but do not require swarm to be active
				return false
			default:
				// ignore any other value for the "swarm" annotation
				return false
			}
		}
		versionOlderThan = func(v string) bool { return versions.LessThan(details.CurrentVersion(), v) }
	)

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		// hide flags not supported by the server
		// root command shows all top-level flags
		if cmd.Parent() != nil {
			if cmds, ok := f.Annotations["top-level"]; ok {
				f.Hidden = !findCommand(cmd, cmds)
			}
			if f.Hidden {
				return
			}
		}

		hideFlagIf(f, notExperimental, "experimental")
		hideFlagIf(f, notOSType, "ostype")
		hideFlagIf(f, notSwarmStatus, "swarm")
		hideFlagIf(f, versionOlderThan, "version")
	})

	for _, subcmd := range cmd.Commands() {
		hideSubcommandIf(subcmd, notExperimental, "experimental")
		hideSubcommandIf(subcmd, notOSType, "ostype")
		hideSubcommandIf(subcmd, notSwarmStatus, "swarm")
		hideSubcommandIf(subcmd, versionOlderThan, "version")
	}
	return nil
}

// Checks if a command or one of its ancestors is in the list
func findCommand(cmd *cobra.Command, cmds []string) bool {
	if cmd == nil {
		return false
	}
	for _, c := range cmds {
		if c == cmd.Name() {
			return true
		}
	}
	return findCommand(cmd.Parent(), cmds)
}

func isSupported(cmd *cobra.Command, details versionDetails) error {
	if err := areSubcommandsSupported(cmd, details); err != nil {
		return err
	}
	return areFlagsSupported(cmd, details)
}

func areFlagsSupported(cmd *cobra.Command, details versionDetails) error {
	errs := []string{}

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if !f.Changed || len(f.Annotations) == 0 {
			return
		}
		// Important: in the code below, calls to "details.CurrentVersion()" and
		// "details.ServerInfo()" are deliberately executed inline to make them
		// be executed "lazily". This is to prevent making a connection with the
		// daemon to perform a "ping" (even for flags that do not require a
		// daemon connection).
		//
		// See commit b39739123b845f872549e91be184cc583f5b387c for details.

		if _, ok := f.Annotations["version"]; ok && !isVersionSupported(f, details.CurrentVersion()) {
			errs = append(errs, fmt.Sprintf(`"--%s" requires API version %s, but the Docker daemon API version is %s`, f.Name, getFlagAnnotation(f, "version"), details.CurrentVersion()))
			return
		}
		if _, ok := f.Annotations["ostype"]; ok && !isOSTypeSupported(f, details.ServerInfo().OSType) {
			errs = append(errs, fmt.Sprintf(
				`"--%s" is only supported on a Docker daemon running on %s, but the Docker daemon is running on %s`,
				f.Name,
				getFlagAnnotation(f, "ostype"), details.ServerInfo().OSType),
			)
			return
		}
		if _, ok := f.Annotations["experimental"]; ok && !details.ServerInfo().HasExperimental {
			errs = append(errs, fmt.Sprintf(`"--%s" is only supported on a Docker daemon with experimental features enabled`, f.Name))
		}
		// buildkit-specific flags are noop when buildkit is not enabled, so we do not add an error in that case
	})
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

func areSubcommandsSupported(cmd *cobra.Command, details versionDetails) error {
	// Check recursively so that, e.g., `docker stack ls` returns the same output as `docker stack`
	for curr := cmd; curr != nil; curr = curr.Parent() {
		// Important: in the code below, calls to "details.CurrentVersion()" and
		// "details.ServerInfo()" are deliberately executed inline to make them
		// be executed "lazily". This is to prevent making a connection with the
		// daemon to perform a "ping" (even for commands that do not require a
		// daemon connection).
		//
		// See commit b39739123b845f872549e91be184cc583f5b387c for details.

		if cmdVersion, ok := curr.Annotations["version"]; ok && versions.LessThan(details.CurrentVersion(), cmdVersion) {
			return fmt.Errorf("%s requires API version %s, but the Docker daemon API version is %s", cmd.CommandPath(), cmdVersion, details.CurrentVersion())
		}
		if ost, ok := curr.Annotations["ostype"]; ok && details.ServerInfo().OSType != "" && ost != details.ServerInfo().OSType {
			return fmt.Errorf("%s is only supported on a Docker daemon running on %s, but the Docker daemon is running on %s", cmd.CommandPath(), ost, details.ServerInfo().OSType)
		}
		if _, ok := curr.Annotations["experimental"]; ok && !details.ServerInfo().HasExperimental {
			return fmt.Errorf("%s is only supported on a Docker daemon with experimental features enabled", cmd.CommandPath())
		}
	}
	return nil
}

func getFlagAnnotation(f *pflag.Flag, annotation string) string {
	if value, ok := f.Annotations[annotation]; ok && len(value) == 1 {
		return value[0]
	}
	return ""
}

func isVersionSupported(f *pflag.Flag, clientVersion string) bool {
	if v := getFlagAnnotation(f, "version"); v != "" {
		return versions.GreaterThanOrEqualTo(clientVersion, v)
	}
	return true
}

func isOSTypeSupported(f *pflag.Flag, osType string) bool {
	if v := getFlagAnnotation(f, "ostype"); v != "" && osType != "" {
		return osType == v
	}
	return true
}

// hasTags return true if any of the command's parents has tags
func hasTags(cmd *cobra.Command) bool {
	for curr := cmd; curr != nil; curr = curr.Parent() {
		if len(curr.Annotations) > 0 {
			return true
		}
	}

	return false
}

func tryRunPluginHelp(dockerCli command.Cli, ccmd *cobra.Command, cargs []string) error {
	root := ccmd.Root()

	cmd, _, err := root.Traverse(cargs)
	if err != nil {
		return err
	}
	helpcmd, err := pluginmanager.PluginRunCommand(dockerCli, cmd.Name(), root)
	if err != nil {
		return err
	}
	return helpcmd.Run()
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
		signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

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

const (
	builderDefaultPlugin = "buildx"
)

func processBuilder(dockerCli command.Cli, cmd *cobra.Command, args, osargs []string) ([]string, []string, []string, error) {
	var envs []string

	fwargs, fwosargs, fwcmdpath, forwarded := forwardBuilder(builderDefaultPlugin, args, osargs)
	if !forwarded {
		return args, osargs, nil, nil
	}

	// If build subcommand is forwarded, user would expect "docker build" to
	// always create a local docker image (default context builder). This is
	// for better backward compatibility in case where a user could switch to
	// a docker container builder with "docker buildx --use foo" which does
	// not --load by default. Also makes sure that an arbitrary builder name
	// is not being set in the command line or in the environment before
	// setting the default context and keep "buildx install" behavior if being
	// set (builder alias).
	if forwarded && !hasBuilderName(args, os.Environ()) {
		envs = append([]string{"BUILDX_BUILDER=" + dockerCli.CurrentContext()}, envs...)
	}

	// overwrite the command path for this plugin using the alias name.
	cmd.Annotations[pluginmanager.CommandAnnotationPluginCommandPath] = strings.Join(append([]string{cmd.CommandPath()}, fwcmdpath...), " ")

	return fwargs, fwosargs, envs, nil
}

func forwardBuilder(alias string, args, osargs []string) ([]string, []string, []string, bool) {
	aliases := [][3][]string{
		{
			{"builder"},
			{alias},
			{"builder"},
		},
		{
			{"build"},
			{alias, "build"},
			{},
		},
		{
			{"image", "build"},
			{alias, "build"},
			{"image"},
		},
	}
	for _, al := range aliases {
		if fwargs, changed := command.StringSliceReplaceAt(args, al[0], al[1], 0); changed {
			fwosargs, _ := command.StringSliceReplaceAt(osargs, al[0], al[1], -1)
			fwcmdpath := al[2]
			return fwargs, fwosargs, fwcmdpath, true
		}
	}
	return args, osargs, nil, false
}

// hasBuilderName checks if a builder name is defined in args or env vars
func hasBuilderName(args []string, envs []string) bool {
	var builder string
	flagset := pflag.NewFlagSet("buildx", pflag.ContinueOnError)
	flagset.Usage = func() {}
	flagset.SetOutput(io.Discard)
	flagset.StringVar(&builder, "builder", "", "")
	_ = flagset.Parse(args)
	if builder != "" {
		return true
	}
	for _, e := range envs {
		if strings.HasPrefix(e, "BUILDX_BUILDER=") && e != "BUILDX_BUILDER=" {
			return true
		}
	}
	return false
}
