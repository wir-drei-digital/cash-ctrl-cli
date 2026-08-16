package main

import (
	"os"
	"strings"
	"testing"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/spec"
)

func parseFixture(t *testing.T) *spec.Spec {
	t.Helper()
	f, err := os.Open("testdata/docs-fixture.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := Parse(f, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseFixture(t *testing.T) {
	s := parseFixture(t)
	if len(s.Operations) != 2 {
		t.Fatalf("got %d operations, want 2 (the intro's base-URL box is not an endpoint)", len(s.Operations))
	}

	list := s.Operations[1]
	if list.Method != "GET" || list.Path != "/widget/list.json" || list.Group != "Widget" || list.Title != "List widgets" {
		t.Fatalf("list = %+v", list)
	}
	if len(list.Params) != 3 {
		t.Fatalf("list params = %+v", list.Params)
	}
	dir := list.Params[0]
	if dir.Name != "dir" || dir.Type != "TEXT" || dir.Required {
		t.Fatalf("dir = %+v", dir)
	}
	// "Possible values" is data, not prose: extracted and stripped.
	if len(dir.Values) != 2 || dir.Values[0] != "ASC" || dir.Values[1] != "DESC" {
		t.Fatalf("dir values = %v", dir.Values)
	}
	if strings.Contains(dir.Doc, "Possible values") {
		t.Fatalf("values left in doc: %q", dir.Doc)
	}

	create := s.Operations[0]
	if create.Method != "POST" || create.Path != "/widget/create.json" {
		t.Fatalf("create = %+v", create)
	}
	if len(create.Params) != 2 {
		t.Fatalf("create params = %+v", create.Params)
	}
	name := create.Params[0]
	if name.Name != "name" || !name.Required {
		t.Fatalf("name = %+v", name)
	}
	parts := create.Params[1]
	if parts.Name != "parts" || parts.Type != "JSON" || len(parts.Sub) != 2 {
		t.Fatalf("parts = %+v", parts)
	}
	// Sub-params stay under their parent, not flattened into the outer list,
	// and their own values are extracted too.
	kind := parts.Sub[0]
	if kind.Name != "kind" || !kind.Required || len(kind.Values) != 2 {
		t.Fatalf("kind = %+v", kind)
	}
	// The sub-table's intro paragraph documents the parent.
	if !strings.Contains(parts.Doc, "JSON array") {
		t.Fatalf("parts doc = %q", parts.Doc)
	}
}

func TestValidate(t *testing.T) {
	s := parseFixture(t)
	if err := Validate(s, 2); err != nil {
		t.Fatal(err)
	}
	if err := Validate(s, 3); err == nil {
		t.Fatal("too-small spec validated")
	}
	dup := &spec.Spec{SpecVersion: spec.Version, Operations: []spec.Operation{
		{Method: "GET", Path: "/a.json"}, {Method: "GET", Path: "/a.json"},
	}}
	if err := Validate(dup, 1); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate not caught: %v", err)
	}
}

// TestVendoredSpecMatchesFixtureExpectations parses the committed spec and
// sanity-checks the properties the rest of the build relies on.
func TestVendoredSpec(t *testing.T) {
	s, err := spec.Load("../../spec/cashctrl-api.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(s, minOperations); err != nil {
		t.Fatal(err)
	}
	var paginated int
	for _, op := range s.Operations {
		var hasStart, hasLimit bool
		for _, p := range op.Params {
			if p.Name == "start" {
				hasStart = true
			}
			if p.Name == "limit" {
				hasLimit = true
			}
		}
		if hasStart && hasLimit {
			paginated++
		}
	}
	if paginated < 30 {
		t.Fatalf("only %d paginated list operations — the page structure likely changed", paginated)
	}
}
