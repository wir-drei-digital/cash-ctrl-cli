package cli

import (
	"strings"
	"testing"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/config"
)

func TestConfigSetAPIKeyFromStdinOnly(t *testing.T) {
	redirectConfig(t)
	ta := newTestApp(t, nil)
	ta.stdin = strings.NewReader("sekret-key\n")
	if code := ta.run([]string{"config", "set", "api-key"}); code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.APIKey != "sekret-key" {
		t.Fatalf("stored key = %q", c.APIKey)
	}

	// From argv it must fail: argv is visible to every process on the machine.
	ta2 := newTestApp(t, nil)
	if code := ta2.run([]string{"config", "set", "api-key", "sekret-key"}); code != 2 {
		t.Fatalf("argv key accepted: exit %d", code)
	}
}

func TestConfigSetOrgValidates(t *testing.T) {
	redirectConfig(t)
	ta := newTestApp(t, nil)
	if code := ta.run([]string{"config", "set", "org", "demo-ag"}); code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	c, _ := config.Load()
	if c.Org != "demo-ag" {
		t.Fatalf("stored org = %q", c.Org)
	}
	ta2 := newTestApp(t, nil)
	if code := ta2.run([]string{"config", "set", "org", "evil.com/x"}); code != 2 {
		t.Fatalf("bad org accepted: exit %d", code)
	}
}

func TestConfigUnsetIsIdempotent(t *testing.T) {
	redirectConfig(t)
	ta := newTestApp(t, nil)
	ta.stdin = strings.NewReader("k\n")
	ta.run([]string{"config", "set", "api-key"})
	if code := ta.run([]string{"config", "unset", "api-key"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	c, _ := config.Load()
	if c.APIKey != "" {
		t.Fatalf("key still stored: %q", c.APIKey)
	}
	// A second unset is a success, not an error.
	if code := ta.run([]string{"config", "unset", "api-key"}); code != 0 {
		t.Fatalf("second unset: exit %d", code)
	}
}

func TestConfigSuggestsKeyForEnvVarName(t *testing.T) {
	redirectConfig(t)
	ta := newTestApp(t, nil)
	code := ta.run([]string{"config", "set", "CASHCTRL_API_KEY"})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(ta.errOut.String(), "config set api-key") {
		t.Fatalf("no suggestion: %s", ta.errOut.String())
	}
}

func TestConfigLangValidated(t *testing.T) {
	redirectConfig(t)
	ta := newTestApp(t, nil)
	if code := ta.run([]string{"config", "set", "lang", "it"}); code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	ta2 := newTestApp(t, nil)
	if code := ta2.run([]string{"config", "set", "lang", "zz"}); code != 2 {
		t.Fatalf("bad lang accepted: exit %d", code)
	}
}

func TestConfigPathPrints(t *testing.T) {
	redirectConfig(t)
	ta := newTestApp(t, nil)
	if code := ta.run([]string{"config", "path"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(ta.out.String(), "config.json") {
		t.Fatalf("stdout = %q", ta.out.String())
	}
}
