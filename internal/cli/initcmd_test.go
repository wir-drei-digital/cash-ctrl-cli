package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/config"
)

// scriptedPrompter answers prompts from a fixed script.
type scriptedPrompter struct {
	answers []string
	secrets []string
}

func (p *scriptedPrompter) Ask(string) (string, error) {
	if len(p.answers) == 0 {
		return "", nil
	}
	a := p.answers[0]
	p.answers = p.answers[1:]
	return a, nil
}

func (p *scriptedPrompter) AskSecret(string) (string, error) {
	if len(p.secrets) == 0 {
		return "", nil
	}
	s := p.secrets[0]
	p.secrets = p.secrets[1:]
	return s, nil
}

func TestInitRefusesWithoutTerminal(t *testing.T) {
	ta := newTestApp(t, nil)
	ta.isTerminal = func() bool { return false }
	code := ta.run([]string{"init"})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	e := ta.stderrJSON(t)
	if e["kind"] != "usage" || !strings.Contains(e["error"].(string), "CASHCTRL_API_KEY") {
		t.Fatalf("refusal = %v", e)
	}
}

func TestInitStoresAndVerifies(t *testing.T) {
	redirectConfig(t)
	srv, c := captureServer(t, `{"total":1,"data":[{"id":1}]}`)
	ta := newTestApp(t, srv)
	ta.isTerminal = func() bool { return true }
	ta.prompt = &scriptedPrompter{answers: []string{"demo"}, secrets: []string{"wizard-key"}}
	// The wizard verifies against BaseFor(org) unless CASHCTRL_API_BASE is
	// set; tests are exactly that case.
	ta.res.FromEnv = map[string]bool{"CASHCTRL_API_BASE": true}
	ta.res.BaseURL = srv.URL + "/api/v1"
	ta.res.AllowCustomBase = true

	code := ta.run([]string{"init"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	if c.path != "/api/v1/currency/list.json" {
		t.Fatalf("verify hit %s", c.path)
	}
	stored, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Org != "demo" || stored.APIKey != "wizard-key" {
		t.Fatalf("stored = %+v", stored)
	}
	var out struct {
		OK  bool   `json:"ok"`
		Org string `json:"org"`
	}
	if err := json.Unmarshal(ta.out.Bytes(), &out); err != nil || !out.OK || out.Org != "demo" {
		t.Fatalf("stdout = %q", ta.out.String())
	}
}

func TestInitRejectsBadOrg(t *testing.T) {
	redirectConfig(t)
	ta := newTestApp(t, nil)
	ta.isTerminal = func() bool { return true }
	ta.prompt = &scriptedPrompter{answers: []string{"Evil.Com/x"}, secrets: []string{"k"}}
	if code := ta.run([]string{"init"}); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
