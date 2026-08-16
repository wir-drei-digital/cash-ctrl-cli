package cli

import (
	"strings"
	"testing"
)

func TestRawAPIGet(t *testing.T) {
	srv, c := captureServer(t, `{"data":{"id":1}}`)
	ta := newTestApp(t, srv)
	code := ta.run([]string{"api", "GET", "/person/read.json", "--query", "id=1"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	if c.path != "/api/v1/person/read.json" || c.query.Get("id") != "1" {
		t.Fatalf("saw %s %v", c.path, c.query)
	}
}

func TestRawAPIRejectsAbsolutePath(t *testing.T) {
	ta := newTestApp(t, nil)
	code := ta.run([]string{"api", "GET", "/api/v1/person/list.json"})
	if code != 2 || !strings.Contains(ta.errOut.String(), "relative") {
		t.Fatalf("exit %d, stderr %s", code, ta.errOut.String())
	}
}

func TestRawAPIRejectsOtherMethods(t *testing.T) {
	ta := newTestApp(t, nil)
	for _, m := range []string{"PUT", "DELETE", "PATCH"} {
		if code := ta.run([]string{"api", m, "/x.json"}); code != 2 {
			t.Fatalf("%s: exit %d, want 2", m, code)
		}
		ta.errOut.Reset()
	}
}

func TestRawAPIUnknownPostNeedsForce(t *testing.T) {
	ta := newTestApp(t, nil)
	code := ta.run([]string{"api", "POST", "/brand/new.json"})
	if code != 2 || !strings.Contains(ta.errOut.String(), "--force") {
		t.Fatalf("exit %d, stderr %s", code, ta.errOut.String())
	}
}

func TestRawAPIKnownWriteNeedsNoForce(t *testing.T) {
	srv, c := captureServer(t, `{"success":true}`)
	ta := newTestApp(t, srv)
	code := ta.run([]string{"api", "POST", "/person/create.json", "--data", `{"firstName":"x"}`})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	if c.form.Get("firstName") != "x" {
		t.Fatalf("form = %v", c.form)
	}
}

func TestRawAPIDeletePathNeedsForceEvenUnknown(t *testing.T) {
	ta := newTestApp(t, nil)
	code := ta.run([]string{"api", "POST", "/brand/new/delete.json"})
	if code != 2 || !strings.Contains(ta.errOut.String(), "--force") {
		t.Fatalf("exit %d, stderr %s", code, ta.errOut.String())
	}
}

func TestRawAPIKnownDeleteInheritsRisk(t *testing.T) {
	ta := newTestApp(t, nil)
	code := ta.run([]string{"api", "POST", "/person/delete.json", "--data", `{"ids":"1"}`})
	if code != 2 || !strings.Contains(ta.errOut.String(), "--force") {
		t.Fatalf("exit %d, stderr %s", code, ta.errOut.String())
	}
}

func TestRawAPIReadOnlyAllowsOnlyGet(t *testing.T) {
	srv, _ := captureServer(t, `{}`)
	ta := newTestApp(t, srv)
	ta.client.ReadOnly = true
	if code := ta.run([]string{"api", "GET", "/person/list.json"}); code != 0 {
		t.Fatalf("GET under read-only: exit %d: %s", code, ta.errOut.String())
	}
	code := ta.run([]string{"api", "POST", "/person/create.json", "--force"})
	if code != 2 || !strings.Contains(ta.errOut.String(), "read-only") {
		t.Fatalf("exit %d, stderr %s", code, ta.errOut.String())
	}
}

func TestRawAPIDataOnGetIsUsage(t *testing.T) {
	ta := newTestApp(t, nil)
	code := ta.run([]string{"api", "GET", "/person/list.json", "--data", `{}`})
	if code != 2 || !strings.Contains(ta.errOut.String(), "--query") {
		t.Fatalf("exit %d, stderr %s", code, ta.errOut.String())
	}
}
