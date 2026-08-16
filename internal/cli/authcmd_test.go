package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/config"
)

type statusOut struct {
	Mode      string `json:"mode"`
	Org       string `json:"org"`
	KeySet    bool   `json:"key_set"`
	KeySource string `json:"key_source"`
	OrgSource string `json:"org_source"`
	Hint      string `json:"hint"`
}

func (ta *testApp) authStatus(t *testing.T) statusOut {
	t.Helper()
	if code := ta.run([]string{"auth", "status"}); code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	var o statusOut
	if err := json.Unmarshal(ta.out.Bytes(), &o); err != nil {
		t.Fatalf("status not JSON (%v): %q", err, ta.out.String())
	}
	return o
}

func TestAuthStatusNone(t *testing.T) {
	ta := newTestApp(t, nil)
	ta.res = config.Resolved{}
	o := ta.authStatus(t)
	if o.Mode != "none" || o.KeySet || o.Hint == "" {
		t.Fatalf("status = %+v", o)
	}
}

func TestAuthStatusKey(t *testing.T) {
	ta := newTestApp(t, nil)
	ta.res = config.Resolved{
		APIKey: "k", Org: "demo", BaseURL: config.BaseFor("demo"),
		FromEnv: map[string]bool{"CASHCTRL_API_KEY": true},
	}
	o := ta.authStatus(t)
	if o.Mode != "key" || o.Org != "demo" || o.KeySource != "env" || o.OrgSource != "config" {
		t.Fatalf("status = %+v", o)
	}
	if o.Hint != "" {
		t.Fatalf("healthy status carries a hint: %+v", o)
	}
	if strings.Contains(ta.out.String(), "k\"") && strings.Contains(ta.out.String(), "\"key\":") {
		t.Fatalf("status leaked the key: %s", ta.out.String())
	}
}

func TestAuthStatusKeyWithoutOrgNamesTheGap(t *testing.T) {
	ta := newTestApp(t, nil)
	ta.res = config.Resolved{APIKey: "k"}
	o := ta.authStatus(t)
	if o.Mode != "none" || !o.KeySet || !strings.Contains(o.Hint, "CASHCTRL_ORG") {
		t.Fatalf("status = %+v", o)
	}
}

func TestAuthVerifyCallsAPI(t *testing.T) {
	srv, c := captureServer(t, `{"total":1,"data":[{"id":1,"code":"CHF"}]}`)
	ta := newTestApp(t, srv)
	if code := ta.run([]string{"auth", "verify"}); code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	if c.path != "/api/v1/currency/list.json" {
		t.Fatalf("verify hit %s", c.path)
	}
	var o struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(ta.out.Bytes(), &o); err != nil || !o.OK {
		t.Fatalf("stdout = %q", ta.out.String())
	}
}

func TestAuthVerifySurfacesAuthFailure(t *testing.T) {
	srv, _ := captureServer(t, `{}`)
	ta := newTestApp(t, srv)
	ta.client.APIKey = ""
	code := ta.run([]string{"auth", "verify"})
	if code != 2 {
		t.Fatalf("exit %d, want 2 (stderr %s)", code, ta.errOut.String())
	}
}
