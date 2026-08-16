package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/spec"
)

// Overrides is the hand-maintained escape hatch for naming and exclusion.
// CashCtrl paths map onto command words mechanically, so unlike bexio there is
// no per-segment naming table: the two remaining knobs are renaming one
// operation and excluding one.
type Overrides struct {
	// Commands replaces the derived command for one operation, keyed by
	// "METHOD /path".
	Commands map[string][]string `json:"commands"`
	// Skiplist excludes operations from the manifest, keyed by "METHOD /path",
	// with a reason that must not be empty.
	Skiplist map[string]string `json:"skiplist"`
}

// LoadOverrides reads the override table from path.
func LoadOverrides(path string) (*Overrides, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var o Overrides
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if o.Commands == nil {
		o.Commands = map[string][]string{}
	}
	if o.Skiplist == nil {
		o.Skiplist = map[string]string{}
	}
	return &o, nil
}

// opKey is how overrides and classifications refer to one operation. CashCtrl
// has no operation ids, and method+path is unique in the spec.
func opKey(op spec.Operation) string { return op.Method + " " + op.Path }

func kebab(s string) string { return strings.ReplaceAll(s, "_", "-") }

// commandPath derives the CLI command from the endpoint path: segments become
// namespaces and the final segment the verb, with non-JSON extensions folded
// into the verb so every documented endpoint stays reachable —
//
//	/person/list.json          -> person list
//	/person/list.csv           -> person list-csv
//	/account/costcenter/balance -> account costcenter balance
//	/file/empty_archive.json   -> file empty-archive
//	/order/document/read.pdf   -> order document read-pdf
func commandPath(op spec.Operation, ov *Overrides) ([]string, error) {
	if c, ok := ov.Commands[opKey(op)]; ok {
		return append([]string{}, c...), nil
	}
	segs := strings.Split(strings.Trim(op.Path, "/"), "/")
	if len(segs) < 2 {
		return nil, fmt.Errorf("%s: path too short for a command (namespace + verb)", opKey(op))
	}
	last := segs[len(segs)-1]
	base, ext, _ := strings.Cut(last, ".")
	verb := kebab(base)
	if ext != "" && ext != "json" {
		verb += "-" + ext
	}
	var words []string
	for _, s := range segs[:len(segs)-1] {
		words = append(words, kebab(s))
	}
	return append(words, verb), nil
}
