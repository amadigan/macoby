package railyard

import (
	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/host/config"
	"github.com/amadigan/macoby/internal/util"
	"github.com/docker/cli/cli/command"
)

var log = applog.New("railyard-cli")

type Cli struct {
	Docker       *command.DockerCli
	ConfigPath   *config.Path
	SearchPath   string
	Suffix       string
	Config       *config.Layout
	Env          map[string]string
	overrideHome string
}

func (c *Cli) setup() error {
	env := util.Env()
	layout, path, err := config.LoadConfig(env, c.overrideHome)
	if err != nil {
		return err
	}

	layout.SetDefaultSockets()

	c.Config = layout
	c.Env = env
	c.ConfigPath = path

	return nil
}
