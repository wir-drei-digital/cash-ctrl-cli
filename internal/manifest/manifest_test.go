package manifest

import (
	"bytes"
	"compress/gzip"
	"testing"
)

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write([]byte(s))
	w.Close()
	return buf.Bytes()
}

func TestParseRejectsWrongSchemaVersion(t *testing.T) {
	if _, err := Parse(gz(t, `{"schema_version":99,"operations":[]}`)); err == nil {
		t.Fatal("schema_version 99 accepted")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("not gzip")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestEmbeddedManifestLoads(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Operations) < 350 {
		t.Fatalf("only %d operations embedded", len(m.Operations))
	}
	for _, op := range m.Operations {
		if op.Method != "GET" && op.Method != "POST" {
			t.Errorf("%s: method %q", op.Path, op.Method)
		}
		if op.Risk != RiskRead && op.Risk != RiskWrite && op.Risk != RiskDelete && op.Risk != RiskSend {
			t.Errorf("%s: risk %q", op.Path, op.Risk)
		}
		if op.Method == "GET" && op.Risk != RiskRead {
			t.Errorf("%s: GET classified %q", op.Path, op.Risk)
		}
		if op.Method == "GET" && op.Body != nil {
			t.Errorf("%s: GET carries a body", op.Path)
		}
		if op.Method == "POST" && op.Body == nil {
			t.Errorf("%s: POST without body spec", op.Path)
		}
	}
}

func TestFind(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	op := m.Find("GET", "/person/list.json")
	if op == nil || op.Pagination != PagStartLimit {
		t.Fatalf("person list: %+v", op)
	}
	if m.Find("POST", "/person/list.json") != nil {
		t.Fatal("method mismatch matched")
	}
	// Query strings and trailing slashes are ignored.
	if m.Find("GET", "/person/list.json?limit=1") == nil {
		t.Fatal("query string broke the match")
	}
	if m.Find("GET", "/nope.json") != nil {
		t.Fatal("unknown path matched")
	}
}
