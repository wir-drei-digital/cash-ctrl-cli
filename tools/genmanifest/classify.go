package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/spec"
)

// riskClasses are the only classes the CLI understands. classifications.json
// is hand-reviewed and decides which commands demand --force, so a typo or an
// invented class must fail the build rather than ship into the manifest as an
// unrecognised — and therefore unguarded — value.
var riskClasses = []string{
	manifest.RiskRead, manifest.RiskWrite, manifest.RiskDelete, manifest.RiskSend,
}

// validRisk reports whether r is one of the four known risk classes.
func validRisk(r string) bool {
	for _, k := range riskClasses {
		if r == k {
			return true
		}
	}
	return false
}

// proposeRisk derives a risk class from the method and path. Proposals are a
// starting point for human review, never the final word: only
// classifications.json decides what ships.
//
//   - GET never mutates in this API: read.
//   - */delete.json deletes; empty_archive.json purges already-archived files
//     for good, which is the most delete a delete gets.
//   - */mail.json sends e-mail to someone outside the org: send.
//   - every other POST changes data but stays inside the org: write.
func proposeRisk(op spec.Operation) string {
	switch {
	case op.Method == "GET":
		return manifest.RiskRead
	case strings.HasSuffix(op.Path, "/delete.json"), strings.HasSuffix(op.Path, "/empty_archive.json"):
		return manifest.RiskDelete
	case strings.HasSuffix(op.Path, "/mail.json"):
		return manifest.RiskSend
	default:
		return manifest.RiskWrite
	}
}

// classify returns the reviewed class from the table, or ("", false) when the
// operation has not been classified yet.
func classify(op spec.Operation, table map[string]string) (string, bool) {
	r, ok := table[opKey(op)]
	return r, ok
}

// LoadClassifications reads the reviewed "METHOD /path" -> risk table.
func LoadClassifications(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}
