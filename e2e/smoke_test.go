// Package e2e drives the real binary as a subprocess. Everything else in this
// repo tests the CLI in-process; this is the only place that proves the
// shipped artifact works — argv parsing, exit codes, stdout/stderr separation
// and the embedded manifest all as the user gets them.
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "cashctrl")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "github.com/wir-drei-digital/cash-ctrl-cli/cmd/cashctrl")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// runEnv executes the binary with a fully controlled environment.
func runEnv(t *testing.T, bin string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

// run executes the binary against the fake server. CASHCTRL_ALLOW_CUSTOM_BASE
// is what lets the key go to a non-cashctrl host at all — the guardrail under
// test everywhere else.
func run(t *testing.T, bin, baseURL string, args ...string) (string, string, int) {
	t.Helper()
	return runEnv(t, bin, append(os.Environ(),
		"CASHCTRL_API_KEY=smoke-key", "CASHCTRL_API_BASE="+baseURL+"/api/v1",
		"CASHCTRL_ALLOW_CUSTOM_BASE=1"), args...)
}

// scrubbedEnv is the environment for the credential tests: every CASHCTRL_*
// variable the developer's shell may carry is dropped, and the config dir is
// redirected into a temp dir. HOME, XDG_CONFIG_HOME and AppData are the full
// set os.UserConfigDir consults on the platforms we ship.
func scrubbedEnv(t *testing.T, extra ...string) []string {
	t.Helper()
	home := t.TempDir()
	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "CASHCTRL_") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, "config"), "AppData="+home)
	return append(env, extra...)
}

// fakeAPI speaks just enough of the CashCtrl dialect: basic auth with the key
// as username, form-encoded POSTs, JSON with success flags and envelopes.
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "smoke-key" || pass != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/person/list.json":
			w.Write([]byte(`{"total":1,"data":[{"id":1,"name":"ACME"}]}`))
		case "/api/v1/person/create.json":
			r.ParseForm()
			if r.PostForm.Get("firstName") == "" && r.PostForm.Get("lastName") == "" && r.PostForm.Get("company") == "" {
				w.Write([]byte(`{"success":false,"errors":[{"field":"lastName","message":"required"}]}`))
				return
			}
			fmt.Fprintf(w, `{"success":true,"insertId":42}`)
		case "/api/v1/currency/list.json":
			w.Write([]byte(`{"total":1,"data":[{"id":1,"code":"CHF"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"not found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSmoke(t *testing.T) {
	bin := buildBinary(t)
	srv := fakeAPI(t)

	stdout, stderr, code := run(t, bin, srv.URL, "person", "list")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var env struct {
		Total int              `json:"total"`
		Data  []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil || len(env.Data) != 1 {
		t.Fatalf("stdout %q", stdout)
	}

	// The force gate must fire before any I/O, so this is a usage error (2).
	_, stderr, code = run(t, bin, srv.URL, "person", "delete", "--data", `{"ids":"1"}`)
	if code != 2 || !strings.Contains(stderr, "--force") {
		t.Fatalf("delete gate: %d %q", code, stderr)
	}

	_, stderr, code = run(t, bin, srv.URL, "tax", "list")
	if code != 1 || !strings.Contains(stderr, `"not_found"`) {
		t.Fatalf("404: %d %q", code, stderr)
	}

	stdout, _, code = run(t, bin, srv.URL, "commands", "--json")
	if code != 0 || !strings.Contains(stdout, `"schema_version":1`) {
		t.Fatalf("catalog: %d", code)
	}
}

// A rejected create must exit 1 even though CashCtrl answers it with HTTP 200:
// the in-band success=false is the failure signal, and an agent reading exit
// codes has to see it.
func TestSmokeSuccessFalse(t *testing.T) {
	bin := buildBinary(t)
	srv := fakeAPI(t)

	stdout, stderr, code := run(t, bin, srv.URL, "person", "create", "--data", `{"altName":"x"}`)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (stdout %q)", code, stdout)
	}
	if stdout != "" {
		t.Fatalf("stdout must stay empty on error, got %q", stdout)
	}
	var e struct {
		Kind   string `json:"kind"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal([]byte(stderr), &e); err != nil {
		t.Fatalf("stderr not JSON (%v): %q", err, stderr)
	}
	if e.Kind != "validation" || e.Status != 200 {
		t.Fatalf("error = %+v", e)
	}

	stdout, _, code = run(t, bin, srv.URL, "person", "create", "--data", `{"lastName":"Muster"}`)
	if code != 0 || !strings.Contains(stdout, "42") {
		t.Fatalf("create: %d %q", code, stdout)
	}
}

func TestSmokeRawAPI(t *testing.T) {
	bin := buildBinary(t)
	srv := fakeAPI(t)

	stdout, stderr, code := run(t, bin, srv.URL, "api", "GET", "/person/list.json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "ACME") {
		t.Fatalf("stdout %q", stdout)
	}

	// An unmatched mutation is treated as dangerous and needs --force.
	_, stderr, code = run(t, bin, srv.URL, "api", "POST", "/brand/new.json")
	if code != 2 || !strings.Contains(stderr, "--force") {
		t.Fatalf("raw mutation gate: %d %q", code, stderr)
	}
}

// Read-only mode has to hold in the shipped binary, not just in unit tests:
// this is the guardrail an operator relies on when handing the CLI to an agent.
func TestSmokeReadOnlyBlocksWrites(t *testing.T) {
	bin := buildBinary(t)
	srv := fakeAPI(t)

	env := append(os.Environ(),
		"CASHCTRL_API_KEY=smoke-key", "CASHCTRL_API_BASE="+srv.URL+"/api/v1",
		"CASHCTRL_ALLOW_CUSTOM_BASE=1", "CASHCTRL_READ_ONLY=1")
	stdout, stderr, code := runEnv(t, bin, env, "person", "create", "--data", `{"lastName":"x"}`)
	if code != 2 || !strings.Contains(stderr, "read-only") {
		t.Fatalf("read-only: %d %q", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout must stay empty on error, got %q", stdout)
	}
}

// Without the opt-in, a custom base is refused before anything is sent: the
// key never reaches a host that is not *.cashctrl.com by accident.
func TestSmokeCustomBaseLockout(t *testing.T) {
	bin := buildBinary(t)
	srv := fakeAPI(t)

	env := append(os.Environ(),
		"CASHCTRL_API_KEY=smoke-key", "CASHCTRL_API_BASE="+srv.URL+"/api/v1")
	stdout, stderr, code := runEnv(t, bin, env, "person", "list")
	if code != 2 || !strings.Contains(stderr, "CASHCTRL_ALLOW_CUSTOM_BASE") {
		t.Fatalf("custom base lockout: %d %q", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout must stay empty, got %q", stdout)
	}
}

// With no credentials at all, an API command must name both ways to get some,
// and `auth status` must still answer with parseable JSON — "no credentials"
// is a legitimate answer to the question it asks.
func TestSmokeNoCreds(t *testing.T) {
	bin := buildBinary(t)

	stdout, stderr, code := runEnv(t, bin, scrubbedEnv(t), "person", "list")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (stderr %q)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout must stay empty on error, got %q", stdout)
	}
	line := strings.TrimSuffix(stderr, "\n")
	if strings.Contains(line, "\n") {
		t.Fatalf("want exactly one line on stderr, got %q", stderr)
	}
	var e struct {
		Kind    string `json:"kind"`
		Message string `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatalf("stderr is not JSON (%v): %q", err, stderr)
	}
	if e.Kind != "usage" || !strings.Contains(e.Message, "CASHCTRL_API_KEY") {
		t.Fatalf("error = %+v", e)
	}

	stdout, stderr, code = runEnv(t, bin, scrubbedEnv(t), "auth", "status")
	if code != 0 {
		t.Fatalf("auth status: exit %d: %s", code, stderr)
	}
	var st struct {
		Mode   string `json:"mode"`
		KeySet bool   `json:"key_set"`
		Hint   string `json:"hint"`
	}
	if err := json.Unmarshal([]byte(stdout), &st); err != nil {
		t.Fatalf("auth status is not JSON (%v): %q", err, stdout)
	}
	if st.Mode != "none" || st.KeySet || st.Hint == "" {
		t.Fatalf("status = %+v", st)
	}
}

// The key must never be readable back out of the binary's output surfaces.
func TestSmokeAuthStatusHidesKey(t *testing.T) {
	bin := buildBinary(t)
	env := scrubbedEnv(t, "CASHCTRL_API_KEY=super-secret-key", "CASHCTRL_ORG=demo")
	stdout, stderr, code := runEnv(t, bin, env, "auth", "status")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if strings.Contains(stdout, "super-secret-key") || strings.Contains(stderr, "super-secret-key") {
		t.Fatal("auth status leaked the key")
	}
	var st struct {
		Mode      string `json:"mode"`
		Org       string `json:"org"`
		KeySource string `json:"key_source"`
	}
	if err := json.Unmarshal([]byte(stdout), &st); err != nil {
		t.Fatalf("not JSON: %q", stdout)
	}
	if st.Mode != "key" || st.Org != "demo" || st.KeySource != "env" {
		t.Fatalf("status = %+v", st)
	}
}

// `cashctrl init` in a subprocess never has a terminal, which makes this the
// one part of the wizard an e2e test can reach — and the part every CI user
// and every agent hits. It must be a usage error that names the way out, not
// a hang waiting on a prompt nobody can answer.
func TestSmokeInitRefusesWithoutTerminal(t *testing.T) {
	bin := buildBinary(t)

	stdout, stderr, code := runEnv(t, bin, scrubbedEnv(t), "init")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (stderr %q)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout must stay empty on error, got %q", stdout)
	}
	var e struct {
		Kind  string `json:"kind"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &e); err != nil {
		t.Fatalf("stderr not JSON (%v): %q", err, stderr)
	}
	if e.Kind != "usage" || !strings.Contains(e.Error, "CASHCTRL_API_KEY") {
		t.Fatalf("refusal = %+v", e)
	}
}

// The upload composite through the shipped binary: prepare → PUT → persist,
// against a fake API and a fake storage host.
func TestSmokeFileUpload(t *testing.T) {
	bin := buildBinary(t)

	var putBytes []byte
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/api/v1/file/prepare.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"success":true,"data":[{"fileId":7,"writeUrl":"%s/storage/x"}]}`, srv.URL)
	})
	mux.HandleFunc("/storage/x", func(w http.ResponseWriter, r *http.Request) {
		putBytes = make([]byte, r.ContentLength)
		r.Body.Read(putBytes)
	})
	mux.HandleFunc("/api/v1/file/persist.json", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.PostForm.Get("ids") != "7" {
			t.Errorf("persist ids = %q", r.PostForm.Get("ids"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true}`))
	})

	path := filepath.Join(t.TempDir(), "hello.txt")
	os.WriteFile(path, []byte("hello e2e"), 0o644)

	stdout, stderr, code := run(t, bin, srv.URL, "file", "upload", path)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if string(putBytes) != "hello e2e" {
		t.Fatalf("storage got %q", putBytes)
	}
	var out struct {
		FileID int64 `json:"file_id"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil || out.FileID != 7 {
		t.Fatalf("stdout = %q", stdout)
	}
}
