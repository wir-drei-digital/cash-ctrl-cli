package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is the released version string, set via -ldflags at build time.
var Version = "dev"

func (a *app) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(a.stdout, "cashctrl %s\n", Version)
		},
	}
}
