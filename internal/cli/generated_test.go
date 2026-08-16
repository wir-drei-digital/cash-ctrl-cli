package cli

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// capture records what the fake server saw.
type capture struct {
	method, path string
	query        url.Values
	form         url.Values
}

func captureServer(t *testing.T, respond string) (*httptest.Server, *capture) {
	t.Helper()
	c := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.method, c.path = r.Method, r.URL.Path
		c.query = r.URL.Query()
		r.ParseForm()
		c.form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(respond))
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func TestGetQueryFlags(t *testing.T) {
	srv, c := captureServer(t, `{"total":0,"data":[]}`)
	ta := newTestApp(t, srv)
	code := ta.run([]string{"person", "list", "--limit", "10", "--only-active", "--query", "acme"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	if c.path != "/api/v1/person/list.json" {
		t.Fatalf("path = %q", c.path)
	}
	if c.query.Get("limit") != "10" || c.query.Get("onlyActive") != "true" || c.query.Get("query") != "acme" {
		t.Fatalf("query = %v", c.query)
	}
	// Only what the caller asked for goes on the wire.
	if c.query.Has("start") || c.query.Has("sort") {
		t.Fatalf("unrequested params on the wire: %v", c.query)
	}
	if got := strings.TrimSpace(ta.out.String()); got != `{"total":0,"data":[]}` {
		t.Fatalf("stdout = %q", got)
	}
}

func TestPostFormEncodesData(t *testing.T) {
	srv, c := captureServer(t, `{"success":true,"insertId":9}`)
	ta := newTestApp(t, srv)
	code := ta.run([]string{"person", "create", "--data",
		`{"firstName":"Ada","categoryId":3,"isEmployee":true,"addresses":[{"type":"MAIN"}],"nr":null}`})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	if c.method != "POST" || c.path != "/api/v1/person/create.json" {
		t.Fatalf("%s %s", c.method, c.path)
	}
	want := map[string]string{
		"firstName":  "Ada",
		"categoryId": "3",
		"isEmployee": "true",
		"addresses":  `[{"type":"MAIN"}]`,
	}
	for k, v := range want {
		if got := c.form.Get(k); got != v {
			t.Errorf("form[%s] = %q, want %q", k, got, v)
		}
	}
	if c.form.Has("nr") {
		t.Error("null value went on the wire")
	}
}

func TestRequiredBodyEnforced(t *testing.T) {
	ta := newTestApp(t, nil)
	code := ta.run([]string{"person", "create"})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(ta.stderrJSON(t)["error"].(string), "--data") {
		t.Fatalf("error = %v", ta.errOut.String())
	}
}

func TestForceGateBlocksDeleteBeforeNetwork(t *testing.T) {
	// No server at all: the gate must fire before any I/O.
	ta := newTestApp(t, nil)
	ta.client.BaseURL = "https://demo.cashctrl.com/api/v1"
	code := ta.run([]string{"person", "delete", "--data", `{"ids":"1"}`})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	e := ta.stderrJSON(t)
	if e["kind"] != "usage" || !strings.Contains(e["error"].(string), "--force") {
		t.Fatalf("error = %v", e)
	}
}

func TestForceGateBlocksSend(t *testing.T) {
	ta := newTestApp(t, nil)
	code := ta.run([]string{"order", "document", "mail", "--data", `{"orderIds":"1"}`})
	if code != 2 || !strings.Contains(ta.errOut.String(), "send-class") {
		t.Fatalf("exit %d, stderr %s", code, ta.errOut.String())
	}
}

func TestForcePassesGate(t *testing.T) {
	srv, c := captureServer(t, `{"success":true}`)
	ta := newTestApp(t, srv)
	code := ta.run([]string{"person", "delete", "--data", `{"ids":"1,2"}`, "--force"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	if c.form.Get("ids") != "1,2" {
		t.Fatalf("form = %v", c.form)
	}
}

func TestReadOnlyBlocksWrite(t *testing.T) {
	srv, _ := captureServer(t, `{}`)
	ta := newTestApp(t, srv)
	ta.client.ReadOnly = true
	code := ta.run([]string{"person", "create", "--data", `{"firstName":"x"}`})
	if code != 2 || !strings.Contains(ta.errOut.String(), "read-only") {
		t.Fatalf("exit %d, stderr %s", code, ta.errOut.String())
	}
}

func TestLangFlagValidatedAndSent(t *testing.T) {
	srv, c := captureServer(t, `{"total":0,"data":[]}`)
	ta := newTestApp(t, srv)
	if code := ta.run([]string{"person", "list", "--lang", "de"}); code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	if c.query.Get("lang") != "de" {
		t.Fatalf("query = %v", c.query)
	}

	ta2 := newTestApp(t, srv)
	if code := ta2.run([]string{"person", "list", "--lang", "xx"}); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestSuccessFalseSurfacesThroughCommand(t *testing.T) {
	srv, _ := captureServer(t, `{"success":false,"errors":[{"field":"name","message":"required"}]}`)
	ta := newTestApp(t, srv)
	code := ta.run([]string{"person", "create", "--data", `{"altName":"x"}`})
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	e := ta.stderrJSON(t)
	if e["kind"] != "validation" {
		t.Fatalf("kind = %v", e["kind"])
	}
	if ta.out.Len() != 0 {
		t.Fatalf("stdout must stay empty on error, got %q", ta.out.String())
	}
}
