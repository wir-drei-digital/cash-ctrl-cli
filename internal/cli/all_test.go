package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// pagedServer serves total items of the form {"id":n} through the
// start/limit envelope, clamping limit at serverMax.
func pagedServer(t *testing.T, total, serverMax int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, _ := strconv.Atoi(r.URL.Query().Get("start"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > serverMax {
			limit = serverMax
		}
		var items []string
		for i := start; i < total && i < start+limit; i++ {
			items = append(items, fmt.Sprintf(`{"id":%d}`, i))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"total":%d,"data":[%s]}`, total, strings.Join(items, ","))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAllMergesPages(t *testing.T) {
	srv := pagedServer(t, 5, 2)
	ta := newTestApp(t, srv)
	code := ta.run([]string{"person", "list", "--all", "--limit", "2"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	var items []struct{ ID int }
	if err := json.Unmarshal(ta.out.Bytes(), &items); err != nil {
		t.Fatalf("stdout is not a JSON array (%v): %q", err, ta.out.String())
	}
	if len(items) != 5 {
		t.Fatalf("got %d items, want 5", len(items))
	}
	for i, it := range items {
		if it.ID != i {
			t.Fatalf("item %d = %+v", i, it)
		}
	}
}

func TestAllServerClampedLimit(t *testing.T) {
	// The CLI asks for pages of 500 by default; the server clamps to 3. The
	// walk must not stop after the first clamped page.
	srv := pagedServer(t, 7, 3)
	ta := newTestApp(t, srv)
	code := ta.run([]string{"person", "list", "--all"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	var items []any
	json.Unmarshal(ta.out.Bytes(), &items)
	if len(items) != 7 {
		t.Fatalf("got %d items, want 7", len(items))
	}
}

func TestAllMaxPagesIsIncomplete(t *testing.T) {
	srv := pagedServer(t, 10, 2)
	ta := newTestApp(t, srv)
	code := ta.run([]string{"person", "list", "--all", "--limit", "2", "--max-pages", "2"})
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	// The partial result is real data and still goes to stdout…
	var items []any
	if err := json.Unmarshal(ta.out.Bytes(), &items); err != nil || len(items) != 4 {
		t.Fatalf("partial stdout: %q (%v)", ta.out.String(), err)
	}
	// …but the run reports incomplete so nobody mistakes it for everything.
	if kind := ta.stderrJSON(t)["kind"]; kind != "incomplete" {
		t.Fatalf("kind = %v", kind)
	}
}

func TestAllRejectsNonEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[1,2,3]`))
	}))
	t.Cleanup(srv.Close)
	ta := newTestApp(t, srv)
	if code := ta.run([]string{"person", "list", "--all"}); code != 2 {
		t.Fatalf("exit %d, want 2 (stderr %s)", code, ta.errOut.String())
	}
}

func TestAllUnsupportedOnUnpaginated(t *testing.T) {
	ta := newTestApp(t, nil)
	// account balance documents no start/limit, so it has no --all flag at all.
	code := ta.run([]string{"account", "balance", "--all"})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(ta.errOut.String(), "unknown flag") {
		t.Fatalf("stderr = %s", ta.errOut.String())
	}
}
