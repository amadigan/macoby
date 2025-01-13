package railyard

import "context"

type setupOpts struct {
	suffix string
}

func runSetup(ctx context.Context, cli *Cli, opts *setupOpts) error {
	if err := cli.setup(); err != nil {
		return err
	}

	return nil
}
