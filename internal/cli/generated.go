package cli

import (
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/api"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/config"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
)

// flagName maps an API parameter name to its CLI flag name: the API speaks
// camelCase, the CLI speaks kebab-case. The original name goes on the wire.
func flagName(param string) string {
	var b strings.Builder
	for i, r := range param {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// groupRunE is what a namespace command does when it is invoked without a
// subcommand. Cobra's default is to print help and exit 0, which reads as
// success to anything checking only the exit code; an incomplete command is a
// usage error and says so.
func groupRunE(cmd *cobra.Command, args []string) error {
	var names []string
	for _, sub := range cmd.Commands() {
		if !sub.Hidden {
			names = append(names, sub.Name())
		}
	}
	return api.Usagef("%s needs a subcommand: %s", cmd.CommandPath(), strings.Join(names, ", "))
}

// addGeneratedCommands hangs one leaf command per manifest operation off root,
// creating the intermediate group commands its command path implies.
func (a *app) addGeneratedCommands(root *cobra.Command) {
	groups := map[string]*cobra.Command{}
	for i := range a.manifest.Operations {
		op := &a.manifest.Operations[i]
		if len(op.Command) < 2 {
			continue // an entry without a namespace has nowhere to hang
		}
		parent := root
		for d := 1; d < len(op.Command); d++ {
			key := strings.Join(op.Command[:d], " ")
			g, ok := groups[key]
			if !ok {
				g = &cobra.Command{
					Use:   op.Command[d-1],
					Short: op.Command[d-1] + " operations",
					RunE:  groupRunE,
				}
				groups[key] = g
				parent.AddCommand(g)
			}
			parent = g
		}
		parent.AddCommand(a.leafCommand(op))
	}
}

func (a *app) leafCommand(op *manifest.Operation) *cobra.Command {
	cmd := &cobra.Command{
		Use:   op.Command[len(op.Command)-1],
		Short: op.Summary,
		Long:  longHelp(op),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runOperation(cmd, op)
		},
	}
	for _, q := range op.Query {
		usage := q.Doc
		if len(q.Values) > 0 {
			usage += " (one of: " + strings.Join(q.Values, ", ") + ")"
		}
		// All flags are strings on purpose: every parameter is a string on
		// the wire, and NUMBER covers decimals ("125.50") an int flag would
		// refuse. BOOLEAN keeps cobra's bool ergonomics.
		if q.Type == "BOOLEAN" {
			cmd.Flags().Bool(flagName(q.Name), false, usage)
		} else {
			cmd.Flags().String(flagName(q.Name), "", usage)
		}
	}
	if op.Body != nil {
		cmd.Flags().String("data", "", "JSON body: literal, @file, or - for stdin (sent form-encoded)")
	}
	if op.Pagination != manifest.PagNone {
		cmd.Flags().Bool("all", false, "fetch every page, emit one merged JSON array")
		cmd.Flags().Int("max-pages", 100, "page cap for --all")
	}
	return cmd
}

// longHelp renders everything needed to call the operation without consulting
// the API docs: the endpoint, its parameters — nested JSON structures
// included — and an example body.
func longHelp(op *manifest.Operation) string {
	var b strings.Builder
	if op.Summary != "" {
		b.WriteString(op.Summary + "\n")
	}
	if op.Doc != "" {
		b.WriteString("\n" + op.Doc + "\n")
	}
	b.WriteString("\n" + op.Method + " " + op.Path + " (risk: " + op.Risk + ")\n")

	if len(op.Query) > 0 {
		b.WriteString("\nQuery parameters (as flags):\n")
		writeParams(&b, op.Query, "  ")
	}
	if op.Body != nil {
		if len(op.Body.Fields) > 0 {
			b.WriteString("\nBody fields (send as JSON via --data; the CLI form-encodes them):\n")
			writeParams(&b, op.Body.Fields, "  ")
		}
		if op.Body.Example != "" {
			b.WriteString("\nExample body:\n  " + op.Body.Example + "\n")
		}
	}
	if op.Pagination != manifest.PagNone {
		b.WriteString("\nPagination: start/limit — --all merges every page into one JSON array.\n")
	}
	if op.Response == manifest.RespBinary {
		b.WriteString("\nResponse is a file download; use --output <file> or redirect stdout.\n")
	}
	return b.String()
}

// writeParams renders a parameter table, recursing into the sub-parameters of
// nested JSON structures with deeper indentation.
func writeParams(b *strings.Builder, ps []manifest.Param, indent string) {
	for _, p := range ps {
		b.WriteString(indent + p.Name + " " + p.Type)
		if p.Required {
			b.WriteString(" (required)")
		}
		if len(p.Values) > 0 {
			b.WriteString(" — one of: " + strings.Join(p.Values, ", "))
		}
		if p.Doc != "" {
			b.WriteString(" — " + p.Doc)
		}
		b.WriteString("\n")
		if len(p.Sub) > 0 {
			writeParams(b, p.Sub, indent+"    ")
		}
	}
}

// runOperation turns one parsed command into one API call.
func (a *app) runOperation(cmd *cobra.Command, op *manifest.Operation) error {
	if all, err := cmd.Flags().GetBool("all"); err == nil && all {
		return a.runAll(cmd, op)
	}
	req, err := a.buildRequest(cmd, op)
	if err != nil {
		return err
	}
	// The gate sits before any I/O: an unconfirmed destructive command must
	// never reach the network.
	if err := a.forceGate(cmd, op); err != nil {
		return err
	}
	if err := a.applyGlobalFlags(cmd); err != nil {
		return err
	}
	resp, err := a.client.Do(cmd.Context(), *req)
	if err != nil {
		return err
	}
	outPath, _ := cmd.Flags().GetString("output")
	return a.writeResponse(op, resp, outPath)
}

func (a *app) buildRequest(cmd *cobra.Command, op *manifest.Operation) (*api.Request, error) {
	req := &api.Request{Method: op.Method, Path: op.Path, Risk: op.Risk}
	q := url.Values{}
	for _, param := range op.Query {
		fn := flagName(param.Name)
		if !cmd.Flags().Changed(fn) {
			continue // only what the caller actually asked for goes on the wire
		}
		if param.Type == "BOOLEAN" {
			v, _ := cmd.Flags().GetBool(fn)
			if v {
				q.Set(param.Name, "true")
			} else {
				q.Set(param.Name, "false")
			}
		} else {
			v, _ := cmd.Flags().GetString(fn)
			q.Set(param.Name, v)
		}
	}
	req.Query = q
	if op.Body != nil {
		body, err := a.readJSONBody(cmd)
		if err != nil {
			return nil, err
		}
		if body == nil && op.Body.Required {
			return nil, api.Usagef("%s requires --data (JSON literal, @file, or - for stdin)", cmd.CommandPath())
		}
		if body != nil {
			form, err := formEncode(body)
			if err != nil {
				return nil, err
			}
			req.Form = form
		} else {
			// A POST without fields still needs a body header to be a POST
			// CashCtrl accepts; an empty form is exactly that.
			req.Form = url.Values{}
		}
	}
	return req, nil
}

func (a *app) applyGlobalFlags(cmd *cobra.Command) error {
	if v, err := cmd.Flags().GetBool("verbose"); err == nil && v {
		a.client.Verbose = a.stderr
	}
	if d, err := cmd.Flags().GetDuration("timeout"); err == nil {
		a.client.Timeout = d
	}
	if l, err := cmd.Flags().GetString("lang"); err == nil && l != "" {
		if !config.ValidLang(l) {
			return api.Usagef("--lang wants one of %s, got %q", strings.Join(config.Languages, ", "), l)
		}
		a.client.Lang = l
	}
	return nil
}

// forceGate refuses delete- and send-class operations that were not confirmed
// with --force. It runs before any network I/O, so an unconfirmed command has
// no side effects at all.
func (a *app) forceGate(cmd *cobra.Command, op *manifest.Operation) error {
	if op.Risk != manifest.RiskDelete && op.Risk != manifest.RiskSend {
		return nil
	}
	if force, _ := cmd.Flags().GetBool("force"); force {
		return nil
	}
	return api.Usagef("%s is a %s-class operation (%s %s); re-run with --force to confirm",
		cmd.CommandPath(), op.Risk, op.Method, op.Path)
}
