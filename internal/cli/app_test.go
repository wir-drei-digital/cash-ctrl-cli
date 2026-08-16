package cli

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/api"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/config"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
)

// testApp is an in-process CLI instance aimed at a fake server, with captured
// stdout/stderr.
type testApp struct {
	*app
	out, errOut *bytes.Buffer
}

// newTestApp builds the app the way Execute does, minus the process plumbing.
func newTestApp(t *testing.T, srv *httptest.Server) *testApp {
	t.Helper()
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	res := config.Resolved{APIKey: "test-key", AllowCustomBase: true}
	if srv != nil {
		res.BaseURL = srv.URL + "/api/v1"
	}
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	return &testApp{
		app: &app{
			manifest: m,
			res:      res,
			client: &api.Client{
				BaseURL: res.BaseURL, APIKey: res.APIKey,
				AllowCustomBase: true, Sleep: func(time.Duration) {},
			},
			stdout: out, stderr: errOut, stdin: strings.NewReader(""),
		},
		out: out, errOut: errOut,
	}
}

// redirectConfig sends the on-disk config into a temp dir through the
// environment, covering every platform os.UserConfigDir consults.
func redirectConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("AppData", home)
}

// stderrJSON decodes the single JSON error line the app leaves on stderr.
func (ta *testApp) stderrJSON(t *testing.T) map[string]any {
	t.Helper()
	line := strings.TrimSuffix(ta.errOut.String(), "\n")
	if line == "" || strings.Contains(line, "\n") {
		t.Fatalf("want exactly one line on stderr, got %q", ta.errOut.String())
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("stderr is not JSON (%v): %q", err, line)
	}
	return m
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	ta := newTestApp(t, nil)
	code := ta.run([]string{"frobnicate"})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if ta.out.Len() != 0 {
		t.Fatalf("stdout must stay empty on error, got %q", ta.out.String())
	}
	if kind := ta.stderrJSON(t)["kind"]; kind != "usage" {
		t.Fatalf("kind = %v", kind)
	}
}

func TestBareGroupIsUsageError(t *testing.T) {
	ta := newTestApp(t, nil)
	code := ta.run([]string{"person"})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	e := ta.stderrJSON(t)
	if !strings.Contains(e["error"].(string), "subcommand") {
		t.Fatalf("error = %v", e["error"])
	}
}

func TestBareRootIsUsageError(t *testing.T) {
	ta := newTestApp(t, nil)
	if code := ta.run(nil); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if ta.out.Len() != 0 {
		t.Fatalf("stdout must stay empty, got %q", ta.out.String())
	}
}
