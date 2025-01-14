package railyard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func NewSetupCommand(cli *Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Setup railyard",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(cli)
		},
	}

	return cmd
}

func runSetup(cli *Cli) error {
	if err := cli.setup(); err != nil {
		return err
	}

	if err := os.MkdirAll(cli.Home, 0755); err != nil {
		return fmt.Errorf("failed to create railyard directory %s: %w", cli.Home, err)
	}

	if err := cli.enableDaemon(); err != nil {
		return fmt.Errorf("failed to enable railyard daemon: %w", err)
	}

	if err := cli.createDockerLink(); err != nil {
		return fmt.Errorf("failed to create docker link: %w", err)
	}

	return nil
}

func (cli *Cli) createDockerLink() error {
	dlpath := filepath.Join(cli.Home, "bin", "docker")

	if _, err := os.Stat(dlpath); err == nil {
		return nil
	}

	if dockerExec, _ := exec.LookPath("docker"); dockerExec != "" {
		return nil
	}

	target, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	if err := os.Symlink(target, dlpath); err != nil {
		return fmt.Errorf("failed to create symlink %s -> %s: %w", dlpath, target, err)
	}

	return nil
}
