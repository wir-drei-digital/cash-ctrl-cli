package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/spec"
)

// repo-relative paths: go test runs with the package dir as cwd.
const (
	specPath            = "../../spec/cashctrl-api.json"
	overridesPath       = "overrides.json"
	classificationsPath = "classifications.json"
	goldenPath          = "testdata/golden_commands.txt"
)

func TestBuildFromVendoredSpec(t *testing.T) {
	m, proposals, err := Build(specPath, overridesPath, classificationsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposals) > 0 {
		t.Fatalf("%d unclassified operations — every operation needs a reviewed risk class", len(proposals))
	}
	if len(m.Operations) < 350 {
		t.Fatalf("only %d operations", len(m.Operations))
	}
	// Every spec operation is either generated or skiplisted; the skiplist is
	// empty today, so the counts must match exactly.
	s, err := spec.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Operations) != len(s.Operations) {
		t.Fatalf("manifest has %d of the spec's %d operations", len(m.Operations), len(s.Operations))
	}
}

// TestGoldenCommands pins each operation's command path, risk class,
// pagination and response kind. A diff here is a breaking change to the CLI's
// public surface and must be deliberate: regenerate with
// UPDATE_GOLDEN=1 go test ./tools/genmanifest — after reading the diff.
func TestGoldenCommands(t *testing.T) {
	m, _, err := Build(specPath, overridesPath, classificationsPath)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for _, op := range m.Operations {
		fmt.Fprintf(&buf, "%s | %s %s | %s | %s | %s\n",
			strings.Join(op.Command, " "), op.Method, op.Path, op.Risk, op.Pagination, op.Response)
	}
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		os.MkdirAll(filepath.Dir(goldenPath), 0o755)
		if err := os.WriteFile(goldenPath, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%v — run UPDATE_GOLDEN=1 go test ./tools/genmanifest once", err)
	}
	if !bytes.Equal(want, buf.Bytes()) {
		t.Fatal("command surface changed; review the diff of golden_commands.txt and regenerate with UPDATE_GOLDEN=1 if intended")
	}
}

func TestCommandPath(t *testing.T) {
	ov := &Overrides{Commands: map[string][]string{}, Skiplist: map[string]string{}}
	cases := []struct {
		method, path string
		want         string
	}{
		{"GET", "/person/list.json", "person list"},
		{"GET", "/person/list.csv", "person list-csv"},
		{"GET", "/person/list.vcf", "person list-vcf"},
		{"POST", "/file/empty_archive.json", "file empty-archive"},
		{"GET", "/account/costcenter/balance", "account costcenter balance"},
		{"GET", "/order/document/read.pdf", "order document read-pdf"},
		{"GET", "/report/collection/download_annualreport.pdf", "report collection download-annualreport-pdf"},
		{"POST", "/journal/import/entry/update_multiple.json", "journal import entry update-multiple"},
		{"GET", "/file/get", "file get"},
	}
	for _, c := range cases {
		got, err := commandPath(spec.Operation{Method: c.method, Path: c.path}, ov)
		if err != nil {
			t.Errorf("%s: %v", c.path, err)
			continue
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("%s -> %q, want %q", c.path, strings.Join(got, " "), c.want)
		}
	}
}

func TestCommandPathOverride(t *testing.T) {
	ov := &Overrides{Commands: map[string][]string{"GET /person/list.json": {"people", "ls"}}}
	got, err := commandPath(spec.Operation{Method: "GET", Path: "/person/list.json"}, ov)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "people ls" {
		t.Fatalf("got %v", got)
	}
}

func TestProposeRisk(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{"GET", "/person/list.json", manifest.RiskRead},
		{"POST", "/person/delete.json", manifest.RiskDelete},
		{"POST", "/file/empty_archive.json", manifest.RiskDelete},
		{"POST", "/order/document/mail.json", manifest.RiskSend},
		{"POST", "/person/create.json", manifest.RiskWrite},
	}
	for _, c := range cases {
		if got := proposeRisk(spec.Operation{Method: c.method, Path: c.path}); got != c.want {
			t.Errorf("%s %s -> %s, want %s", c.method, c.path, got, c.want)
		}
	}
}

func TestClassificationsAreComplete(t *testing.T) {
	table, err := LoadClassifications(classificationsPath)
	if err != nil {
		t.Fatal(err)
	}
	// The delete- and send-class entries are the CLI's safety surface; pin
	// the counts so an accidental mass-edit is caught.
	counts := map[string]int{}
	for _, r := range table {
		counts[r]++
	}
	if counts["send"] != 3 {
		t.Errorf("send-class operations = %d, want 3 (the mail endpoints)", counts["send"])
	}
	if counts["delete"] != 45 {
		t.Errorf("delete-class operations = %d, want 45 (44 delete.json + empty_archive)", counts["delete"])
	}
	if counts["read"] == 0 || counts["write"] == 0 {
		t.Errorf("counts = %v", counts)
	}
}

func TestBuildFailsOnStaleEntries(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "classifications.json")
	os.WriteFile(stale, []byte(`{"GET /gone/away.json":"read"}`), 0o644)
	_, _, err := Build(specPath, overridesPath, stale)
	if err == nil || !strings.Contains(err.Error(), "matches no operation") {
		t.Fatalf("stale classification not reported: %v", err)
	}
}

func TestBuildFailsOnInvalidRisk(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "classifications.json")
	os.WriteFile(bad, []byte(`{"GET /person/list.json":"wirte"}`), 0o644)
	_, _, err := Build(specPath, overridesPath, bad)
	if err == nil || !strings.Contains(err.Error(), "wirte") {
		t.Fatalf("invalid risk not reported: %v", err)
	}
}

func TestExampleSkeleton(t *testing.T) {
	got := example([]spec.Param{
		{Name: "ids", Type: "CSV", Required: true},
		{Name: "optional", Type: "TEXT"},
		{Name: "items", Type: "JSON", Required: true, Doc: "This is a JSON array [{...}] with parameters:",
			Sub: []spec.Param{{Name: "accountId", Type: "NUMBER", Required: true}}},
	})
	want := `{"ids":"1,2,3","items":[{"accountId":0}]}`
	if got != want {
		t.Fatalf("example = %s, want %s", got, want)
	}
}
