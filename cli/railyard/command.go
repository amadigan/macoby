package railyard

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/host/config"
	"github.com/amadigan/macoby/internal/util"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var log = applog.New("railyard-cli")

type Cli struct {
	Docker     *command.DockerCli
	Home       string
	SearchPath string
	Suffix     string
	Config     *config.Layout
	Env        map[string]string
}

func (c *Cli) setup() error {
	env := util.Env()
	home, searchpath := config.BuildHomePath(env, c.Home)
	env[config.HomeEnv] = searchpath
	c.Home = home
	c.SearchPath = searchpath

	if defaultHome := os.ExpandEnv(config.UserHomeDir); home != defaultHome {
		c.Suffix = filepath.Base(home)
	}

	confPath := &config.Path{Original: fmt.Sprintf("${%s}/%s.yaml", config.HomeEnv, config.Name)}

	if !confPath.ResolveInputFile(env, home) {
		return fmt.Errorf("failed to find %s.yaml", config.Name)
	}

	log.Infof("Using config file: %s", confPath.Resolved)

	var layout config.Layout

	bs, err := os.ReadFile(confPath.Resolved)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", confPath.Resolved, err)
	}

	if err := yaml.Unmarshal(bs, &layout); err != nil {
		return fmt.Errorf("failed to unmarshal %s: %w", confPath.Resolved, err)
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
	cmd.AddCommand(NewSetupCommand(cli))

	return cmd
}
