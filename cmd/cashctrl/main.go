// Command cashctrl is a CLI for the CashCtrl API, built for agents and
// scripts: JSON in, JSON out, one static binary, no runtime dependencies.
package main

import (
	"os"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/cli"
)

func main() { os.Exit(cli.Execute(os.Args[1:])) }
