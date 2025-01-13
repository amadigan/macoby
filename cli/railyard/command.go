package railyard

import (
	"fmt"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/config"
	"github.com/amadigan/macoby/internal/util"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

var log = applog.New("railyard-cli")

type Cli struct {
	Docker     *command.DockerCli
	Home       string
	SearchPath string
	Config     *config.Layout
	Env        map[string]string
}

func (c *Cli) setup() error {
	env := util.Env()
	home, searchpath := config.BuildHomePath(env, c.Home)
	env[config.HomeEnv] = searchpath
	c.Home = home
	c.SearchPath = searchpath
	confPath := &config.Path{Original: fmt.Sprintf("${%s}/%s.jsonc", config.HomeEnv, config.Name)}

	if !confPath.ResolveInputFile(env, home) {
		return fmt.Errorf("failed to find %s.jsonc", config.Name)
	}

	var layout config.Layout
	if err := util.ReadJsonConfig(confPath.Resolved, &layout); err != nil {
		return fmt.Errorf("failed to read railyard.json: %w", err)
	}

	layout.SetDefaults()
	c.Config = &layout
	c.Env = env

	layout.SetDefaultSockets()

	return nil
}

func NewVMCommand(cli *Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vm",
		Short: "Manage railyard VM",
	}

	cmd.PersistentFlags().StringVarP(&cli.Home, "home", "H", "", "railyard home directory")

	cmd.AddCommand(NewDebugCommand(cli))
	cmd.AddCommand(NewEnableCommand(cli))
	cmd.AddCommand(NewDisableCommand(cli))

	return cmd
}
