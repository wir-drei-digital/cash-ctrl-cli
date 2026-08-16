// Package config resolves the CLI's credentials and endpoint settings from
// the config file and the environment. Environment variables always win.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

// userConfigDir is swapped in tests.
var userConfigDir = os.UserConfigDir

// Languages CashCtrl accepts for the lang parameter.
var Languages = []string{"de", "en", "fr", "it"}

// ValidLang reports whether l is a language CashCtrl accepts.
func ValidLang(l string) bool {
	for _, v := range Languages {
		if l == v {
			return true
		}
	}
	return false
}

// orgPattern is what a CashCtrl organization subdomain may look like. The org
// is spliced into a URL, so anything beyond DNS-label characters is refused
// before it can redirect credentials elsewhere ("evil.com/" would).
var orgPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidOrg reports whether org can safely form <org>.cashctrl.com.
func ValidOrg(org string) bool { return orgPattern.MatchString(org) }

// Config is the on-disk configuration.
type Config struct {
	APIKey   string `json:"api_key,omitempty"`
	Org      string `json:"org,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
	Lang     string `json:"lang,omitempty"`
}

// Path returns the config file location, <os.UserConfigDir()>/cashctrl/config.json.
func Path() (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cashctrl", "config.json"), nil
}

// Load reads the config file. A missing file yields the zero Config and no
// error; a corrupt file is a real error.
func Load() (Config, error) {
	p, err := Path()
	if err != nil {
		return Config{}, err
	}
	raw, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return Config{}, fmt.Errorf("%s is corrupt: %w", p, err)
	}
	return c, nil
}

// Save writes the config file with 0600 perms inside a 0700 directory.
// The write is temp-file-then-rename so a concurrent reader can never see a
// partial file.
func Save(c Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Same directory as the target: rename is only atomic within a filesystem,
	// and CreateTemp already makes the file 0600.
	tmp, err := os.CreateTemp(filepath.Dir(p), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	// fsync before the rename: rename is atomic, not durable. Without this a
	// crash shortly after Save can leave the renamed file empty — a stored
	// API key gone without anyone deleting it.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return err
	}
	// Best effort: directory fsync is not portable (Windows refuses), and a
	// config that is written but not yet durable is better than a failed Save.
	if d, err := os.Open(filepath.Dir(p)); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// Resolved is the effective configuration after the environment overlay.
type Resolved struct {
	APIKey   string
	Org      string
	// BaseURL is the full API base including /api/v1. Empty when neither an
	// org nor CASHCTRL_API_BASE is configured — the client's guard turns that
	// into a usage error naming both.
	BaseURL         string
	ReadOnly        bool
	AllowCustomBase bool
	Lang            string
	// FromEnv names the CASHCTRL_* variables that were set non-empty when this
	// was resolved. Names only, never values: several carry credentials.
	// A nil map still answers, so no consumer needs a nil check.
	FromEnv map[string]bool
}

// BaseFor returns the API base URL for an organization subdomain.
func BaseFor(org string) string { return "https://" + org + ".cashctrl.com/api/v1" }

// Resolve loads the config file and overlays the environment: CASHCTRL_API_KEY,
// CASHCTRL_ORG, CASHCTRL_API_BASE and CASHCTRL_LANG win when non-empty,
// CASHCTRL_READ_ONLY=1 forces read-only, and CASHCTRL_ALLOW_CUSTOM_BASE=1
// permits sending the key to a host that is not *.cashctrl.com.
func Resolve(getenv func(string) string) (Resolved, error) {
	c, err := Load()
	if err != nil {
		return Resolved{}, err
	}
	r := Resolved{APIKey: c.APIKey, Org: c.Org, ReadOnly: c.ReadOnly, Lang: c.Lang}
	env := func(name string) string {
		v := getenv(name)
		if v != "" {
			if r.FromEnv == nil {
				r.FromEnv = map[string]bool{}
			}
			r.FromEnv[name] = true
		}
		return v
	}
	if v := env("CASHCTRL_API_KEY"); v != "" {
		r.APIKey = v
	}
	if v := env("CASHCTRL_ORG"); v != "" {
		r.Org = v
	}
	if r.Org != "" && !ValidOrg(r.Org) {
		return Resolved{}, fmt.Errorf("org %q is not a valid organization subdomain (lowercase letters, digits, hyphens)", r.Org)
	}
	if r.Org != "" {
		r.BaseURL = BaseFor(r.Org)
	}
	// A full base URL wins over the org-derived one: it exists for tests and
	// proxies, and the client refuses a non-cashctrl.com host unless
	// CASHCTRL_ALLOW_CUSTOM_BASE=1 is also set.
	if v := env("CASHCTRL_API_BASE"); v != "" {
		r.BaseURL = v
	}
	if v := env("CASHCTRL_LANG"); v != "" {
		r.Lang = v
	}
	if r.Lang != "" && !ValidLang(r.Lang) {
		return Resolved{}, fmt.Errorf("lang %q is not one of de, en, fr, it", r.Lang)
	}
	// Recorded as set whatever they hold, not only on "1": FromEnv answers
	// "did the environment speak", and a CASHCTRL_READ_ONLY=0 that this
	// ignores is still a variable someone set and may need to be told about.
	if env("CASHCTRL_READ_ONLY") == "1" {
		r.ReadOnly = true
	}
	if env("CASHCTRL_ALLOW_CUSTOM_BASE") == "1" {
		r.AllowCustomBase = true
	}
	return r, nil
}
