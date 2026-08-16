package cli

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/api"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
)

// apiCommand is the escape hatch for endpoints the manifest does not cover.
// It is deliberately not a bypass: every guardrail still applies, and an
// endpoint the CLI cannot classify is treated as the more dangerous option.
func (a *app) apiCommand() *cobra.Command {
	var queries, headers []string
	cmd := &cobra.Command{
		Use:   "api <METHOD> <path>",
		Short: "Raw authenticated request (escape hatch); guardrails still apply",
		Long: "Raw authenticated request against any CashCtrl endpoint.\n\n" +
			"The path is relative to /api/v1 and must start with /, e.g. /person/list.json.\n" +
			"CashCtrl speaks GET and POST only. A --data JSON object is form-encoded the\n" +
			"same way generated commands encode it.\n\n" +
			"Guardrails still apply: a path the manifest knows inherits that operation's\n" +
			"risk class, an unknown POST requires --force, a POST to a */delete.json or\n" +
			"*/empty_archive.json path always requires --force, and read-only mode\n" +
			"permits GET only.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			method := strings.ToUpper(args[0])
			path := args[1]
			if method != http.MethodGet && method != http.MethodPost {
				return api.Usagef("method %q not allowed (the CashCtrl API speaks GET and POST)", args[0])
			}
			if !strings.HasPrefix(path, "/") {
				return api.Usagef("path must start with / (e.g. /person/list.json), got %q", path)
			}
			if strings.HasPrefix(path, "/api/v1/") {
				// The base URL already ends in /api/v1; a caller pasting the
				// documented absolute path would silently hit /api/v1/api/v1/….
				return api.Usagef("path is relative to /api/v1; use %q", strings.TrimPrefix(path, "/api/v1"))
			}

			// Read-only is checked first: telling the caller to add --force
			// when the request would be refused anyway is bad advice.
			if a.client.ReadOnly && method != http.MethodGet {
				return api.Usagef("read-only mode: cashctrl api allows only GET")
			}

			risk := manifest.RiskWrite
			matched := a.manifest.Find(method, path)
			switch {
			case matched != nil:
				risk = matched.Risk
			case method == http.MethodGet:
				risk = manifest.RiskRead
			case strings.HasSuffix(path, "/delete.json"), strings.HasSuffix(path, "/empty_archive.json"):
				risk = manifest.RiskDelete
			case strings.HasSuffix(path, "/mail.json"):
				risk = manifest.RiskSend
			}
			// An endpoint the manifest cannot classify gets the cautious
			// treatment: unknown mutations are confirmed like a deletion.
			unmatchedMutation := matched == nil && method != http.MethodGet
			needsForce := risk == manifest.RiskDelete || risk == manifest.RiskSend || unmatchedMutation
			if force, _ := cmd.Flags().GetBool("force"); needsForce && !force {
				reason := "risk: " + risk
				if unmatchedMutation && risk == manifest.RiskWrite {
					reason = "unmatched endpoint, treated as a mutation"
				}
				return api.Usagef("cashctrl api %s %s requires --force (%s)", method, path, reason)
			}

			q := url.Values{}
			for _, kv := range queries {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					return api.Usagef("--query wants k=v, got %q", kv)
				}
				q.Add(k, v)
			}
			hdr := http.Header{}
			for _, kv := range headers {
				k, v, ok := strings.Cut(kv, ":")
				if !ok || strings.TrimSpace(k) == "" {
					return api.Usagef("--header wants k:v, got %q", kv)
				}
				hdr.Add(strings.TrimSpace(k), strings.TrimSpace(v))
			}
			body, err := a.readJSONBody(cmd)
			if err != nil {
				return err
			}

			// A GET is a read whatever the matched operation says, so read-only
			// mode never blocks it; anything else keeps the class it inherited.
			effectiveRisk := risk
			if method == http.MethodGet {
				effectiveRisk = manifest.RiskRead
			}
			req := api.Request{Method: method, Path: path, Query: q, Risk: effectiveRisk, Headers: hdr}
			if method == http.MethodPost {
				req.Form = url.Values{}
				if body != nil {
					form, err := formEncode(body)
					if err != nil {
						return err
					}
					req.Form = form
				}
			} else if body != nil {
				return api.Usagef("--data is for POST; GET parameters go in --query")
			}
			if err := a.applyGlobalFlags(cmd); err != nil {
				return err
			}
			resp, err := a.client.Do(cmd.Context(), req)
			if err != nil {
				return err
			}
			outPath, _ := cmd.Flags().GetString("output")
			// The response shape is unknown here. Marking it JSON lets the
			// content type decide whether a trailing newline is safe.
			return a.writeResponse(&manifest.Operation{Response: manifest.RespJSON}, resp, outPath)
		},
	}
	cmd.Flags().String("data", "", "JSON body for POST: literal, @file, or - for stdin (sent form-encoded)")
	cmd.Flags().StringArrayVar(&queries, "query", nil, "query param k=v (repeatable)")
	cmd.Flags().StringArrayVar(&headers, "header", nil, "extra header k:v (repeatable; Authorization is not overridable)")
	return cmd
}
