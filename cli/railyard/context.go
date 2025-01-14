package railyard

import (
	"fmt"
	"strings"

	"github.com/amadigan/macoby/internal/config"
	"github.com/containerd/errdefs"
	"github.com/docker/cli/cli/command"
	dcontext "github.com/docker/cli/cli/context"
	"github.com/docker/cli/cli/context/docker"
	"github.com/docker/cli/cli/context/store"
)

func (cli *Cli) upsertContext() error {
	name := config.Name + cli.Suffix

	dockerSock := cli.Config.DockerSocket.HostPath[0]

	network, addr, err := dockerSock.ResolveListenSocket(cli.Env, cli.Home)
	if err != nil {
		return fmt.Errorf("failed to resolve docker socket: %w", err)
	}

	var dockerHost string

	switch {
	case network == "unix":
		dockerHost = "unix://" + addr
	case strings.HasPrefix(addr, ":"):
		dockerHost = "tcp://localhost" + addr
	default:
		dockerHost = "tcp://" + addr
	}

	log.Infof("expected docker host: %s", dockerHost)

	store := cli.Docker.ContextStore()

	meta, err := store.GetMetadata(name)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			log.Warnf("failed to get metadata for %s: %v", name, err)
		}
	} else {
		meta.Name = name
	}

	dm := meta.Endpoints[docker.DockerEndpoint]

	log.Infof("context %s: %T %+v", name, dm, dm)

	if emb, ok := dm.(dcontext.EndpointMetaBase); ok && emb.Host == dockerHost {
		log.Infof("context %s already up to date", name)

		return nil
	}

	meta.Endpoints[docker.DockerEndpoint] = dcontext.EndpointMetaBase{Host: dockerHost}

	if err := store.CreateOrUpdate(meta); err != nil {
		return fmt.Errorf("failed to update context %s: %w", name, err)
	}

	return nil
}

func (cli *Cli) selectContext() error {
	name := config.Name + cli.Suffix

	var configValue string // default is ""

	if name != command.DefaultContextName {
		if err := store.ValidateContextName(name); err != nil {
			return fmt.Errorf("invalid context name %s: %w", name, err)
		}

		if _, err := cli.Docker.ContextStore().GetMetadata(name); err != nil {
			return fmt.Errorf("context %s does not exist: %w", name, err)
		}

		configValue = name
	}

	dockerConfig := cli.Docker.ConfigFile()

	if dockerConfig.CurrentContext != configValue {
		dockerConfig.CurrentContext = configValue

		if err := dockerConfig.Save(); err != nil {
			return fmt.Errorf("failed to save docker config: %w", err)
		}
	}

	return nil
}

func deleteContext(cli *Cli) error {
	name := config.Name + cli.Suffix

	if err := cli.Docker.ContextStore().Remove(name); err != nil {
		return fmt.Errorf("failed to delete context %s: %w", name, err)
	}

	return nil
}
