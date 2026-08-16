package cli

import (
	"time"

	"github.com/spf13/cobra"
)

func (a *app) newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "cashctrl",
		Short: "CLI for the CashCtrl API, built for agents: JSON in, JSON out",
		Long: "CLI for the CashCtrl API, built for agents: JSON in, JSON out.\n\n" +
			"stdout carries the API response and nothing else; errors are one line of JSON\n" +
			"on stderr. Exit codes: 0 success, 1 API/network error, 2 usage error.\n\n" +
			"Run `cashctrl commands --json` for the machine-readable catalog of every operation.",
		// The root is a namespace like any other: `cashctrl` alone names no
		// operation. Cobra's default is to print help on stdout and exit 0,
		// which both breaks the stdout contract (only an API response belongs
		// there) and reads as success to anything checking the exit code.
		// `cashctrl --help` still prints the overview, on the help path where
		// it belongs.
		RunE:          groupRunE,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().Bool("verbose", false, "log requests to stderr (API key redacted)")
	root.PersistentFlags().Duration("timeout", 30*time.Second, "per-attempt HTTP timeout")
	root.PersistentFlags().Bool("force", false, "confirm delete- and send-class operations")
	root.PersistentFlags().String("output", "", "write response body to file instead of stdout")
	root.PersistentFlags().String("lang", "", "response language for errors and documents (de, en, fr, it)")
	root.AddCommand(a.initCommand(), a.versionCommand(), a.commandsCommand(),
		a.configCommand(), a.authCommand(), a.apiCommand())
	a.addGeneratedCommands(root)
	a.attachUpload(root)
	return root
}
