package cli

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/api"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
)

// defaultPageSize is the page size --all requests when the caller did not pick
// one: large enough to keep the round trips down, small enough that a single
// response stays manageable. CashCtrl's own default is 100.
const defaultPageSize = 500

// runAll walks every page of a paginated list and writes ONE merged JSON
// array — the contents of every page's "data", exactly as the API sent each
// item. This is the only place the CLI reshapes a response.
//
// Hitting --max-pages is not silent success: the partial array still goes to
// stdout (it is real data) and the run ends with an "incomplete" error so a
// caller cannot mistake a truncated result for the whole set.
func (a *app) runAll(cmd *cobra.Command, op *manifest.Operation) error {
	base, err := a.buildRequest(cmd, op)
	if err != nil {
		return err
	}
	if err := a.forceGate(cmd, op); err != nil {
		return err
	}
	if err := a.applyGlobalFlags(cmd); err != nil {
		return err
	}
	maxPages, _ := cmd.Flags().GetInt("max-pages")

	limit := defaultPageSize
	if cmd.Flags().Changed("limit") {
		if v, _ := cmd.Flags().GetString("limit"); v != "" {
			l, err := strconv.Atoi(v)
			if err != nil || l <= 0 {
				return api.Usagef("--limit wants a positive integer with --all, got %q", v)
			}
			limit = l
		}
	}

	var items []json.RawMessage
	complete := false
	start := 0
	// pageSize is the short-page threshold, and it is the size the SERVER
	// used, not the one we asked for: a server that clamps our limit answers
	// with shorter-but-full pages, and treating its first one as short would
	// silently truncate the result. The first response reveals the real size;
	// total (which CashCtrl sends on every list) usually ends the walk before
	// the short-page rule is ever needed.
	pageSize := 0
	for page := 0; page < maxPages; page++ {
		req := *base
		q := cloneValues(base.Query)
		q.Set("limit", strconv.Itoa(limit))
		q.Set("start", strconv.Itoa(start))
		req.Query = q
		resp, err := a.client.Do(cmd.Context(), req)
		if err != nil {
			return err
		}
		var env struct {
			Total *int              `json:"total"`
			Data  []json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(resp.Body, &env); err != nil || env.Data == nil {
			return api.Usagef("--all expects a {\"total\", \"data\"} envelope, got: %.100s", resp.Body)
		}
		items = append(items, env.Data...)
		start += len(env.Data)
		if len(env.Data) == 0 || (env.Total != nil && start >= *env.Total) {
			complete = true
			break
		}
		if page == 0 {
			pageSize = len(env.Data)
		} else if len(env.Data) < pageSize {
			// Shorter than the server's own page size: the last page. Only
			// reachable when the envelope carried no total, which costs one
			// extra request when the first page is already the end — cheaper
			// than reporting a clamped page as the whole result set.
			complete = true
			break
		}
	}

	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(it)
	}
	buf.WriteString("]\n")
	// Whichever sink the merged array goes to, the incomplete signal outlives
	// it: a truncated file that exits 0 reads as complete data.
	if outPath, _ := cmd.Flags().GetString("output"); outPath != "" {
		if err := a.writeResponse(op, &api.Response{Body: buf.Bytes()}, outPath); err != nil {
			return err
		}
	} else {
		a.stdout.Write(buf.Bytes())
	}

	if !complete {
		return &api.Error{
			Kind:    api.KindIncomplete,
			Message: "hit --max-pages before exhausting results; output on stdout is partial",
		}
	}
	return nil
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string{}, vs...)
	}
	return out
}
