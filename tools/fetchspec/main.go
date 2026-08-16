// Command fetchspec extracts the CashCtrl API surface from the published help
// page and vendors it as spec/cashctrl-api.json. CashCtrl publishes no OpenAPI
// document; the generated help page is the closest thing to a spec, and its
// structure is stable enough to parse.
//
// The spec is vendored so a broken extraction can never break a build: this
// tool validates what it extracted (operation count, methods, duplicates) and
// refuses to write anything that fails those checks.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// minOperations is the smallest operation count a real extraction can have.
// The page documents ~376 today; a parse that finds far fewer means the page
// changed shape and the parser needs work, not that the API shrank.
const minOperations = 350

const defaultSource = "https://app.cashctrl.com/static/help/en/api/index.html"

// Provenance records where and when the vendored spec came from, so a diff of
// the spec is traceable to a fetch.
type Provenance struct {
	Source     string `json:"source"`
	FetchedAt  string `json:"fetched_at"`
	PageSHA256 string `json:"page_sha256"`
	Operations int    `json:"operations"`
}

func run() error {
	src := flag.String("src", defaultSource, "URL of the CashCtrl API help page")
	fromFile := flag.String("from-file", "", "parse a local HTML file instead of fetching (for tests)")
	out := flag.String("out", "spec/cashctrl-api.json", "path of the vendored spec")
	prov := flag.String("provenance", "spec/PROVENANCE.json", "path of the provenance record")
	flag.Parse()

	var page []byte
	if *fromFile != "" {
		var err error
		if page, err = os.ReadFile(*fromFile); err != nil {
			return err
		}
	} else {
		resp, err := http.Get(*src)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s: HTTP %d", *src, resp.StatusCode)
		}
		if page, err = io.ReadAll(resp.Body); err != nil {
			return err
		}
	}

	s, err := Parse(bytes.NewReader(page), *src)
	if err != nil {
		return err
	}
	if err := Validate(s, minOperations); err != nil {
		return fmt.Errorf("refusing to write spec: %w", err)
	}

	raw, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	p := Provenance{
		Source:     *src,
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
		PageSHA256: fmt.Sprintf("%x", sha256.Sum256(page)),
		Operations: len(s.Operations),
	}
	rawP, err := json.MarshalIndent(p, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*prov, append(rawP, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d operations\n", *out, len(s.Operations))
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
