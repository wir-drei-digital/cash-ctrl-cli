package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// redirect sends the config into a temp dir for the duration of one test.
func redirect(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := userConfigDir
	userConfigDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userConfigDir = orig })
	return dir
}

func noEnv(string) string { return "" }

func TestLoadMissingFileIsZero(t *testing.T) {
	redirect(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c != (Config{}) {
		t.Fatalf("got %+v", c)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	redirect(t)
	in := Config{APIKey: "k", Org: "demo", ReadOnly: true, Lang: "de"}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v, want %+v", out, in)
	}
	if runtime.GOOS != "windows" {
		p, _ := Path()
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("config file perm = %o, want 600", perm)
		}
		dirInfo, err := os.Stat(filepath.Dir(p))
		if err != nil {
			t.Fatal(err)
		}
		if perm := dirInfo.Mode().Perm(); perm != 0o700 {
			t.Errorf("config dir perm = %o, want 700", perm)
		}
	}
}

func TestCorruptFileIsError(t *testing.T) {
	dir := redirect(t)
	p := filepath.Join(dir, "cashctrl", "config.json")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte("{nope"), 0o600)
	if _, err := Load(); err == nil {
		t.Fatal("corrupt config loaded without error")
	}
}

func TestResolveEnvWins(t *testing.T) {
	redirect(t)
	Save(Config{APIKey: "file-key", Org: "fileorg", Lang: "de"})
	env := map[string]string{
		"CASHCTRL_API_KEY": "env-key",
		"CASHCTRL_ORG":     "envorg",
		"CASHCTRL_LANG":    "fr",
	}
	r, err := Resolve(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if r.APIKey != "env-key" || r.Org != "envorg" || r.Lang != "fr" {
		t.Fatalf("got %+v", r)
	}
	if r.BaseURL != "https://envorg.cashctrl.com/api/v1" {
		t.Fatalf("BaseURL = %q", r.BaseURL)
	}
	for _, name := range []string{"CASHCTRL_API_KEY", "CASHCTRL_ORG", "CASHCTRL_LANG"} {
		if !r.FromEnv[name] {
			t.Errorf("FromEnv[%s] = false", name)
		}
	}
}

func TestResolveNoOrgMeansNoBaseURL(t *testing.T) {
	redirect(t)
	r, err := Resolve(noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if r.BaseURL != "" {
		t.Fatalf("BaseURL = %q, want empty", r.BaseURL)
	}
}

func TestResolveAPIBaseOverridesOrg(t *testing.T) {
	redirect(t)
	Save(Config{Org: "demo"})
	r, err := Resolve(func(k string) string {
		if k == "CASHCTRL_API_BASE" {
			return "http://127.0.0.1:1/api/v1"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.BaseURL != "http://127.0.0.1:1/api/v1" {
		t.Fatalf("BaseURL = %q", r.BaseURL)
	}
}

func TestResolveRejectsBadOrg(t *testing.T) {
	redirect(t)
	// An org that could smuggle a path or another host into the base URL must
	// fail resolution, not become a URL.
	for _, bad := range []string{"evil.com/x", "a b", "UPPER", "org?x=1"} {
		_, err := Resolve(func(k string) string {
			if k == "CASHCTRL_ORG" {
				return bad
			}
			return ""
		})
		if err == nil {
			t.Errorf("org %q resolved without error", bad)
		}
	}
}

func TestResolveRejectsBadLang(t *testing.T) {
	redirect(t)
	_, err := Resolve(func(k string) string {
		if k == "CASHCTRL_LANG" {
			return "xx"
		}
		return ""
	})
	if err == nil {
		t.Fatal("lang xx resolved without error")
	}
}

func TestResolveFlags(t *testing.T) {
	redirect(t)
	r, err := Resolve(func(k string) string {
		switch k {
		case "CASHCTRL_READ_ONLY":
			return "1"
		case "CASHCTRL_ALLOW_CUSTOM_BASE":
			return "1"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.ReadOnly || !r.AllowCustomBase {
		t.Fatalf("got %+v", r)
	}
}
