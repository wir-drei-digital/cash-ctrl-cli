package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogJSON(t *testing.T) {
	ta := newTestApp(t, nil)
	if code := ta.run([]string{"commands", "--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	var out struct {
		SchemaVersion int `json:"schema_version"`
		Commands      []struct {
			Command    string `json:"command"`
			Method     string `json:"method"`
			Path       string `json:"path"`
			Risk       string `json:"risk"`
			Pagination string `json:"pagination"`
			Response   string `json:"response"`
			Body       *struct {
				Required bool   `json:"required"`
				Example  string `json:"example"`
			} `json:"body"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(ta.out.Bytes(), &out); err != nil {
		t.Fatalf("catalog not JSON: %v", err)
	}
	if out.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d", out.SchemaVersion)
	}
	if len(out.Commands) < 350 {
		t.Fatalf("only %d commands", len(out.Commands))
	}
	byCmd := map[string]int{}
	for i, c := range out.Commands {
		byCmd[c.Command] = i
	}
	// Spot checks against known operations.
	i, ok := byCmd["person list"]
	if !ok {
		t.Fatal("person list missing")
	}
	if c := out.Commands[i]; c.Method != "GET" || c.Path != "/person/list.json" ||
		c.Risk != "read" || c.Pagination != "start-limit" || c.Response != "json" {
		t.Fatalf("person list = %+v", c)
	}
	i, ok = byCmd["person delete"]
	if !ok {
		t.Fatal("person delete missing")
	}
	if c := out.Commands[i]; c.Risk != "delete" || c.Body == nil || !c.Body.Required ||
		!strings.Contains(c.Body.Example, "ids") {
		t.Fatalf("person delete = %+v", c)
	}
	i, ok = byCmd["order document mail"]
	if !ok {
		t.Fatal("order document mail missing")
	}
	if c := out.Commands[i]; c.Risk != "send" {
		t.Fatalf("order document mail risk = %s", out.Commands[i].Risk)
	}
	i, ok = byCmd["person list-csv"]
	if !ok {
		t.Fatal("person list-csv missing")
	}
	if c := out.Commands[i]; c.Response != "binary" {
		t.Fatalf("person list-csv response = %s", c.Response)
	}
}

func TestCatalogPlain(t *testing.T) {
	ta := newTestApp(t, nil)
	if code := ta.run([]string{"commands"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(ta.out.String(), "person list") {
		t.Fatal("plain catalog misses person list")
	}
}
