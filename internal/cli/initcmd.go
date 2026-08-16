package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/api"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/config"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
)

// initCommand is the interactive setup wizard: it collects the organization
// and the API key, stores them with 0600 permissions, and proves they work
// with one read-only API call. It is the one command that needs a terminal —
// everywhere else the CLI is strictly non-interactive.
func (a *app) initCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive setup: organization + API key, verified with one read-only call",
		Long: "Interactive setup for the cashctrl CLI.\n\n" +
			"Asks for the organization subdomain and the API key (input hidden), stores\n" +
			"both in the config file with 0600 permissions, and proves they work with one\n" +
			"read-only API call. Create the API user under Settings > Users & Roles in\n" +
			"CashCtrl; the API is available on the PRO plan.\n\n" +
			"Needs a terminal. For scripts and agents, use the environment instead:\n" +
			"CASHCTRL_ORG and CASHCTRL_API_KEY.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			isTerm := a.isTerminal
			if isTerm == nil {
				isTerm = stdinIsTerminal
			}
			if !isTerm() {
				return api.Usagef("cashctrl init is interactive and needs a terminal; " +
					"set CASHCTRL_ORG and CASHCTRL_API_KEY (or use `cashctrl config set`) instead")
			}
			prompt := a.prompt
			if prompt == nil {
				prompt = terminalPrompter{}
			}

			// Values the environment holds win over anything this wizard
			// writes, so saying it up front beats a config file that
			// mysteriously does not take effect.
			for _, v := range []string{"CASHCTRL_ORG", "CASHCTRL_API_KEY"} {
				if a.res.FromEnv[v] {
					fmt.Fprintf(a.stderr, "note: %s is set in this environment and will keep winning over the config file\n", v)
				}
			}

			orgPrompt := "Organization subdomain (the myorg in myorg.cashctrl.com): "
			if a.res.Org != "" {
				orgPrompt = fmt.Sprintf("Organization subdomain [%s]: ", a.res.Org)
			}
			org, err := prompt.Ask(orgPrompt)
			if err != nil {
				return api.Usagef("%v", err)
			}
			if org == "" {
				org = a.res.Org
			}
			if !config.ValidOrg(org) {
				return api.Usagef("org %q is not a valid organization subdomain (lowercase letters, digits, hyphens)", org)
			}
			key, err := prompt.AskSecret("API key (input hidden; from Settings > Users & Roles): ")
			if err != nil {
				return api.Usagef("%v", err)
			}
			if key == "" {
				return api.Usagef("empty API key")
			}

			c, err := config.Load()
			if err != nil {
				return api.Usagef("%v", err)
			}
			c.Org, c.APIKey = org, key
			if err := config.Save(c); err != nil {
				return api.Usagef("%v", err)
			}
			fmt.Fprintln(a.stderr, "saved; verifying with one read-only call…")

			// Verify with the values just collected, not the resolved config:
			// an environment override would test the wrong credential. The
			// custom-base escape stays honored so tests can point elsewhere.
			verify := &api.Client{
				BaseURL: config.BaseFor(org), APIKey: key, Lang: a.res.Lang,
				AllowCustomBase: a.res.AllowCustomBase, Sleep: a.client.Sleep,
				Timeout: a.client.Timeout, HTTP: a.client.HTTP,
			}
			if a.res.FromEnv["CASHCTRL_API_BASE"] {
				verify.BaseURL = a.res.BaseURL
			}
			if _, err := verify.Do(cmd.Context(), api.Request{
				Method: "GET", Path: verifyPath, Risk: manifest.RiskRead,
			}); err != nil {
				fmt.Fprintln(a.stderr, "verification failed — the org and key are saved, but the API refused them:")
				return err
			}
			return json.NewEncoder(a.stdout).Encode(struct {
				OK  bool   `json:"ok"`
				Org string `json:"org"`
			}{true, org})
		},
	}
}
