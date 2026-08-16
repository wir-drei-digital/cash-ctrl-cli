// Command genmanifest turns the vendored spec into the manifest embedded in
// the binary. Operations whose risk class has not been human-reviewed are
// reported as proposals and fail the build: risk classes decide which commands
// demand --force, so nothing ships unreviewed.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/spec"
)

// Proposal is a machine-guessed risk class awaiting human review.
type Proposal struct{ Key, Proposed string }

// Build assembles the full manifest. It collects every problem it finds —
// command collisions, stale skiplist entries, invalid classes — and reports
// them together, so one run shows all the work needed instead of surfacing
// the next problem only after the previous one is fixed.
func Build(specPath, overridesPath, classificationsPath string) (*manifest.Manifest, []Proposal, error) {
	s, err := spec.Load(specPath)
	if err != nil {
		return nil, nil, err
	}
	ov, err := LoadOverrides(overridesPath)
	if err != nil {
		return nil, nil, err
	}
	table, err := LoadClassifications(classificationsPath)
	if err != nil {
		return nil, nil, err
	}

	var problems []string
	var proposals []Proposal
	var ops []manifest.Operation
	seen := map[string]string{}
	skipped := map[string]bool{}
	inSpec := map[string]bool{}

	// Check the whole table, not just the entries this run happens to use, so
	// one run reports every bad class an engineer needs to fix.
	for key, risk := range table {
		if !validRisk(risk) {
			problems = append(problems, fmt.Sprintf("classification %q has risk %q — want one of %s",
				key, risk, strings.Join(riskClasses, ", ")))
		}
	}

	for _, raw := range s.Operations {
		key := opKey(raw)
		inSpec[key] = true
		if reason, skip := ov.Skiplist[key]; skip {
			skipped[key] = true
			if strings.TrimSpace(reason) == "" {
				problems = append(problems, fmt.Sprintf("skiplist entry %s has empty reason", key))
			}
			continue
		}
		cmd, err := commandPath(raw, ov)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		cmdKey := strings.Join(cmd, " ")
		if prev, dup := seen[cmdKey]; dup {
			problems = append(problems, fmt.Sprintf("command collision %q: %s vs %s", cmdKey, prev, key))
			continue
		}
		seen[cmdKey] = key
		risk, ok := classify(raw, table)
		if !ok {
			proposals = append(proposals, Proposal{key, proposeRisk(raw)})
			continue
		}
		op := manifest.Operation{
			Command: cmd, Method: raw.Method, Path: raw.Path, Group: raw.Group,
			Summary: raw.Title, Doc: truncate(raw.Doc, docLimit), Risk: risk,
			Body: bodySpec(raw), Pagination: pagination(raw), Response: responseKind(raw),
		}
		if raw.Method == "GET" {
			op.Query = params(raw.Params)
		}
		ops = append(ops, op)
	}

	// Table or skiplist entries that match nothing are rot from a past spec
	// revision; classification rot also hides a rename, which is a breaking
	// change that must be seen.
	for key := range table {
		if !inSpec[key] {
			problems = append(problems, fmt.Sprintf("classification %q matches no operation in the spec — remove it", key))
		}
	}
	for key := range ov.Skiplist {
		if !skipped[key] {
			problems = append(problems, fmt.Sprintf("skiplist entry %q matches no operation in the spec — remove it", key))
		}
	}
	for key := range ov.Commands {
		if !inSpec[key] {
			problems = append(problems, fmt.Sprintf("command override %q matches no operation in the spec — remove it", key))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, proposals, fmt.Errorf("generator problems:\n  %s", strings.Join(problems, "\n  "))
	}
	sort.Slice(ops, func(i, j int) bool {
		return strings.Join(ops[i].Command, " ") < strings.Join(ops[j].Command, " ")
	})
	return &manifest.Manifest{SchemaVersion: manifest.SchemaVersion, Operations: ops}, proposals, nil
}

// encode returns the manifest's canonical JSON and its gzipped form.
func encode(m *manifest.Manifest) (raw, gz []byte, err error) {
	raw, err = json.Marshal(m)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, nil, err
	}
	if _, err := zw.Write(raw); err != nil {
		return nil, nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, nil, err
	}
	return raw, buf.Bytes(), nil
}

func main() {
	specPath := flag.String("spec", "spec/cashctrl-api.json", "path to the vendored spec")
	overrides := flag.String("overrides", "tools/genmanifest/overrides.json", "path to the override table")
	classifications := flag.String("classifications", "tools/genmanifest/classifications.json", "path to the reviewed risk table")
	out := flag.String("out", "internal/manifest/manifest.json.gz", "path of the generated manifest")
	check := flag.Bool("check", false, "verify the committed manifest matches the spec instead of writing it")
	writeProposals := flag.Bool("write-proposals", false, "write classifications.proposed.json for review and exit")
	flag.Parse()

	m, proposals, err := Build(*specPath, *overrides, *classifications)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(proposals) > 0 {
		if *writeProposals {
			merged, err := LoadClassifications(*classifications)
			if err != nil || merged == nil {
				merged = map[string]string{}
			}
			for _, p := range proposals {
				merged[p.Key] = p.Proposed
			}
			raw, err := json.MarshalIndent(merged, "", "  ")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			// Sits beside the table it proposes for, so a run with a custom
			// -classifications path cannot quietly write somewhere else.
			dest := strings.TrimSuffix(*classifications, ".json") + ".proposed.json"
			if err := os.WriteFile(dest, append(raw, '\n'), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "wrote %d proposals to %s — review, then rename to classifications.json\n", len(proposals), dest)
		} else {
			for _, p := range proposals {
				fmt.Fprintf(os.Stderr, "unclassified: %-55s (proposed: %s)\n", p.Key, p.Proposed)
			}
			fmt.Fprintln(os.Stderr, "run with -write-proposals, review, rename to classifications.json")
		}
		os.Exit(1)
	}

	rawJSON, gz, err := encode(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *check {
		existing, err := os.ReadFile(*out)
		if err != nil {
			fmt.Fprintln(os.Stderr, "check:", err)
			os.Exit(1)
		}
		zr, err := gzip.NewReader(bytes.NewReader(existing))
		if err != nil {
			fmt.Fprintln(os.Stderr, "check:", err)
			os.Exit(1)
		}
		existingJSON, err := io.ReadAll(zr)
		if err != nil {
			fmt.Fprintln(os.Stderr, "check:", err)
			os.Exit(1)
		}
		if !bytes.Equal(existingJSON, rawJSON) {
			fmt.Fprintln(os.Stderr, "manifest drift: run `make generate` and commit")
			os.Exit(1)
		}
		fmt.Println("manifest up to date")
		return
	}
	if err := os.WriteFile(*out, gz, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s: %d operations, %d bytes gz\n", *out, len(m.Operations), len(gz))
}
