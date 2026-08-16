package cli

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/api"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/config"
)

func (a *app) configCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Manage the cashctrl config file", RunE: groupRunE}
	set := &cobra.Command{
		Use:   "set",
		Short: "Set a config value",
		// ArbitraryArgs so an env-var-shaped key reaches keyGroupRunE and gets
		// the config key it plainly means, instead of cobra's unknown-command
		// error.
		Args: cobra.ArbitraryArgs,
		RunE: keyGroupRunE("set"),
	}

	set.AddCommand(&cobra.Command{
		Use:   "api-key",
		Short: "Read the API key from stdin and save it (0600)",
		Long: "Read the API key from stdin and save it (0600).\n\n" +
			"The key is never taken from the command line — argv is visible to every\n" +
			"process on the machine. Usage: echo $KEY | cashctrl config set api-key",
		// NoArgs is the guardrail: `cashctrl config set api-key <secret>` must
		// fail rather than silently accept a key from argv.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc := bufio.NewScanner(a.stdin)
			if !sc.Scan() {
				return api.Usagef("no API key on stdin; usage: echo $KEY | cashctrl config set api-key")
			}
			key := strings.TrimSpace(sc.Text())
			if key == "" {
				return api.Usagef("empty API key")
			}
			c, err := config.Load()
			if err != nil {
				return api.Usagef("%v", err)
			}
			c.APIKey = key
			if err := config.Save(c); err != nil {
				return api.Usagef("%v", err)
			}
			fmt.Fprintln(a.stdout, "api key saved")
			return nil
		},
	})

	set.AddCommand(&cobra.Command{
		Use:   "org <subdomain>",
		Short: "Save the organization subdomain (the myorg in myorg.cashctrl.com)",
		Long: "Save the organization subdomain — the myorg in https://myorg.cashctrl.com.\n\n" +
			"An API key belongs to exactly one organization, so key and org travel as a\n" +
			"pair. The org is an identifier, not a secret, so it may come from argv.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			org := strings.TrimSpace(args[0])
			if !config.ValidOrg(org) {
				return api.Usagef("org %q is not a valid organization subdomain (lowercase letters, digits, hyphens)", org)
			}
			c, err := config.Load()
			if err != nil {
				return api.Usagef("%v", err)
			}
			c.Org = org
			if err := config.Save(c); err != nil {
				return api.Usagef("%v", err)
			}
			fmt.Fprintf(a.stdout, "org = %s\n", c.Org)
			return nil
		},
	})

	set.AddCommand(&cobra.Command{
		Use:   "read-only <true|false>",
		Short: "Persist read-only mode",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parsed strictly: a typo must not quietly leave writes enabled.
			v, err := strconv.ParseBool(args[0])
			if err != nil {
				return api.Usagef("read-only wants true or false, got %q", args[0])
			}
			c, err := config.Load()
			if err != nil {
				return api.Usagef("%v", err)
			}
			c.ReadOnly = v
			if err := config.Save(c); err != nil {
				return api.Usagef("%v", err)
			}
			fmt.Fprintf(a.stdout, "read_only = %v\n", c.ReadOnly)
			return nil
		},
	})

	set.AddCommand(&cobra.Command{
		Use:   "lang <de|en|fr|it>",
		Short: "Persist the response language for errors and generated documents",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := strings.ToLower(strings.TrimSpace(args[0]))
			if !config.ValidLang(l) {
				return api.Usagef("lang wants one of %s, got %q", strings.Join(config.Languages, ", "), args[0])
			}
			c, err := config.Load()
			if err != nil {
				return api.Usagef("%v", err)
			}
			c.Lang = l
			if err := config.Save(c); err != nil {
				return api.Usagef("%v", err)
			}
			fmt.Fprintf(a.stdout, "lang = %s\n", c.Lang)
			return nil
		},
	})

	// unset is set's other half: without it a value written into the config
	// file could only be removed by editing the file by hand.
	unset := &cobra.Command{
		Use:   "unset",
		Short: "Remove a config value",
		Args:  cobra.ArbitraryArgs, // same reason as set's, above
		RunE:  keyGroupRunE("unset"),
	}
	for _, k := range []struct {
		use, short string
		stored     func(config.Config) bool
		clear      func(*config.Config)
	}{
		{
			use:   "api-key",
			short: "Remove the stored API key (CASHCTRL_API_KEY in the environment is untouched)",
			stored: func(c config.Config) bool { return c.APIKey != "" },
			clear:  func(c *config.Config) { c.APIKey = "" },
		},
		{
			use:   "org",
			short: "Remove the stored organization subdomain",
			stored: func(c config.Config) bool { return c.Org != "" },
			clear:  func(c *config.Config) { c.Org = "" },
		},
		{
			use:   "read-only",
			short: "Stop persisting read-only mode (CASHCTRL_READ_ONLY=1 still forces it)",
			stored: func(c config.Config) bool { return c.ReadOnly },
			clear:  func(c *config.Config) { c.ReadOnly = false },
		},
		{
			use:   "lang",
			short: "Remove the stored response language",
			stored: func(c config.Config) bool { return c.Lang != "" },
			clear:  func(c *config.Config) { c.Lang = "" },
		},
	} {
		unset.AddCommand(&cobra.Command{
			Use:   k.use,
			Short: k.short,
			// NoArgs: unsetting takes no value, and `config unset api-key
			// <something>` is a user who means something else.
			Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := config.Load()
				if err != nil {
					return api.Usagef("%v", err)
				}
				// Nothing stored is a success, not an error: "make sure this is
				// not in the file" is the whole point, so a second run has to be
				// a no-op. No Save either — a config file that does not exist
				// yet must not be created to record an absence.
				if !k.stored(c) {
					fmt.Fprintf(a.stdout, "%s was not set\n", k.use)
					return nil
				}
				k.clear(&c)
				if err := config.Save(c); err != nil {
					return api.Usagef("%v", err)
				}
				fmt.Fprintf(a.stdout, "%s removed\n", k.use)
				return nil
			},
		})
	}

	path := &cobra.Command{
		Use:   "path",
		Short: "Print the config file location",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := config.Path()
			if err != nil {
				return api.Usagef("%v", err)
			}
			fmt.Fprintln(a.stdout, p)
			return nil
		},
	}

	cmd.AddCommand(set, unset, path)
	return cmd
}

// keyGroupRunE answers `config set` / `config unset` invoked with something
// that is not one of their subcommands. An env-var-shaped key is the expected
// mistake rather than a typo — the two namings sit side by side in the docs —
// so it is answered with the config key it plainly means.
func keyGroupRunE(verb string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return groupRunE(cmd, args)
		}
		if k := suggestConfigKey(args[0]); k != "" {
			return api.Usagef("config keys are api-key, org, read-only and lang — "+
				"not environment variable names; did you mean `cashctrl config %s %s`?", verb, k)
		}
		return groupRunE(cmd, args)
	}
}

// suggestConfigKey maps an env-var-shaped or underscored name onto the config
// key it plainly means.
func suggestConfigKey(given string) string {
	switch strings.TrimPrefix(strings.ToLower(given), "cashctrl_") {
	case "api_key", "apikey":
		return "api-key"
	case "org":
		return "org"
	case "read_only", "readonly":
		return "read-only"
	case "lang":
		return "lang"
	}
	return ""
}
