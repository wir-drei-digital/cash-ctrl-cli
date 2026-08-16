package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/api"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
)

// verifyPath is the read-only call `auth verify` makes: small, present in
// every org, and requiring no parameters.
const verifyPath = "/currency/list.json"

func (a *app) authCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Inspect and verify credentials", RunE: groupRunE}

	// status answers "what would authenticate right now" without any network:
	// the cheap first check before diagnosing an auth failure. The key itself
	// never appears in the output.
	status := &cobra.Command{
		Use:   "status",
		Short: "Report the active credential setup as one line of JSON (no network, no secrets)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			type out struct {
				Mode      string `json:"mode"` // "key" or "none"
				Org       string `json:"org,omitempty"`
				KeySet    bool   `json:"key_set"`
				KeySource string `json:"key_source,omitempty"` // "env" or "config"
				OrgSource string `json:"org_source,omitempty"`
				Hint      string `json:"hint,omitempty"`
			}
			o := out{Mode: "none", Org: a.res.Org, KeySet: a.res.APIKey != ""}
			if o.KeySet {
				o.KeySource = "config"
				if a.res.FromEnv["CASHCTRL_API_KEY"] {
					o.KeySource = "env"
				}
			}
			if a.res.Org != "" {
				o.OrgSource = "config"
				if a.res.FromEnv["CASHCTRL_ORG"] {
					o.OrgSource = "env"
				}
			}
			switch {
			case o.KeySet && (a.res.Org != "" || a.res.FromEnv["CASHCTRL_API_BASE"]):
				o.Mode = "key"
			case o.KeySet:
				o.Hint = "API key is set but no organization: set CASHCTRL_ORG or run `cashctrl config set org <org>`"
			case a.res.Org != "":
				o.Hint = "organization is set but no API key: set CASHCTRL_API_KEY or run `echo $KEY | cashctrl config set api-key`"
			default:
				o.Hint = "no credentials: run `cashctrl init`, or set CASHCTRL_ORG and CASHCTRL_API_KEY"
			}
			return json.NewEncoder(a.stdout).Encode(o)
		},
	}

	// verify proves the configured credentials against the real API with one
	// cheap read-only call, and reports the result in the same JSON dialect
	// every other command uses.
	verify := &cobra.Command{
		Use:   "verify",
		Short: "Prove the credentials with one read-only API call (" + verifyPath + ")",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.applyGlobalFlags(cmd); err != nil {
				return err
			}
			_, err := a.client.Do(cmd.Context(), api.Request{
				Method: "GET", Path: verifyPath, Risk: manifest.RiskRead,
			})
			if err != nil {
				return err
			}
			return json.NewEncoder(a.stdout).Encode(struct {
				OK  bool   `json:"ok"`
				Org string `json:"org,omitempty"`
			}{OK: true, Org: a.res.Org})
		},
	}

	cmd.AddCommand(status, verify)
	return cmd
}
