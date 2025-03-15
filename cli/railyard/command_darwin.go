package railyard

import (
	"github.com/spf13/cobra"
)

func NewVMCommand(cli *Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vm",
		Short: "Manage railyard VM",
	}

	cmd.PersistentFlags().StringVarP(&cli.overrideHome, "home", "H", "", "railyard home directory")

	cmd.AddCommand(NewDebugCommand(cli))
	cmd.AddCommand(NewEnableCommand(cli))
	cmd.AddCommand(NewDisableCommand(cli))
	cmd.AddCommand(NewStatsCommand(cli))

	return cmd
}
