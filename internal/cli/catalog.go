package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
)

// catalogEntry is one operation as `cashctrl commands --json` publishes it. It
// is a stable, snake_case projection of the manifest: the schema_version
// alongside it is the contract a consumer pins to.
type catalogEntry struct {
	Command    string           `json:"command"`
	Method     string           `json:"method"`
	Path       string           `json:"path"`
	Group      string           `json:"group,omitempty"`
	Risk       string           `json:"risk"`
	Summary    string           `json:"summary,omitempty"`
	Doc        string           `json:"doc,omitempty"`
	Pagination string           `json:"pagination"`
	Response   string           `json:"response"`
	Query      []manifest.Param `json:"query,omitempty"`
	Body       *manifest.Body   `json:"body,omitempty"`
}

func (a *app) commandsCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "commands",
		Short: "List every available API command (--json for the machine-readable catalog)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON {
				out := struct {
					SchemaVersion int            `json:"schema_version"`
					Commands      []catalogEntry `json:"commands"`
				}{SchemaVersion: manifest.SchemaVersion, Commands: []catalogEntry{}}
				for _, op := range a.manifest.Operations {
					out.Commands = append(out.Commands, catalogEntry{
						Command: strings.Join(op.Command, " "), Method: op.Method, Path: op.Path,
						Group: op.Group, Risk: op.Risk, Summary: op.Summary, Doc: op.Doc,
						Pagination: op.Pagination, Response: op.Response,
						Query: op.Query, Body: op.Body,
					})
				}
				return json.NewEncoder(a.stdout).Encode(out)
			}
			for _, op := range a.manifest.Operations {
				fmt.Fprintf(a.stdout, "%-55s %s %s [%s]\n",
					strings.Join(op.Command, " "), op.Method, op.Path, op.Risk)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable catalog")
	return cmd
}
