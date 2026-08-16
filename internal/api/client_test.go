package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
)

// testClient returns a client aimed at srv with retries that do not sleep.
func testClient(srv *httptest.Server) *Client {
	return &Client{
		BaseURL: srv.URL + "/api/v1", APIKey: "test-key", AllowCustomBase: true,
		Sleep: func(time.Duration) {},
	}
}

func get(path string) Request {
	return Request{Method: "GET", Path: path, Risk: manifest.RiskRead}
}

func TestBasicAuthHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := testClient(srv)
	if _, err := c.Do(context.Background(), get("/x.json")); err != nil {
		t.Fatal(err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("test-key:"))
	if got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
}

func TestGuardNoKey(t *testing.T) {
	c := &Client{BaseURL: "https://demo.cashctrl.com/api/v1"}
	_, err := c.Do(context.Background(), get("/x.json"))
	assertKind(t, err, KindUsage, "CASHCTRL_API_KEY")
}

func TestGuardNoOrg(t *testing.T) {
	c := &Client{APIKey: "k"}
	_, err := c.Do(context.Background(), get("/x.json"))
	assertKind(t, err, KindUsage, "CASHCTRL_ORG")
}

func TestGuardCustomBaseLockout(t *testing.T) {
	c := &Client{APIKey: "k", BaseURL: "https://api.example.com/api/v1"}
	_, err := c.Do(context.Background(), get("/x.json"))
	assertKind(t, err, KindUsage, "CASHCTRL_ALLOW_CUSTOM_BASE")
}

func TestGuardRefusesPlainHTTPExceptLoopback(t *testing.T) {
	c := &Client{APIKey: "k", BaseURL: "http://demo.cashctrl.com/api/v1"}
	_, err := c.Do(context.Background(), get("/x.json"))
	assertKind(t, err, KindUsage, "non-HTTPS")

	// Loopback is fine: that is how every test in this repo talks to a fake.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if _, err := testClient(srv).Do(context.Background(), get("/x.json")); err != nil {
		t.Fatal(err)
	}
}

func TestGuardReadOnlyBlocksWrites(t *testing.T) {
	c := &Client{APIKey: "k", BaseURL: "https://demo.cashctrl.com/api/v1", ReadOnly: true}
	_, err := c.Do(context.Background(), Request{Method: "POST", Path: "/x.json", Risk: manifest.RiskWrite})
	assertKind(t, err, KindUsage, "read-only")
	// The block happens locally: no server was involved at all.
}

func TestLangTravelsAsQueryParam(t *testing.T) {
	var gotLang string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLang = r.URL.Query().Get("lang")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := testClient(srv)
	c.Lang = "fr"
	if _, err := c.Do(context.Background(), get("/x.json")); err != nil {
		t.Fatal(err)
	}
	if gotLang != "fr" {
		t.Fatalf("lang = %q, want fr", gotLang)
	}
}

func TestFormBodyEncoding(t *testing.T) {
	var gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		gotBody = string(b)
		w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()
	form := url.Values{}
	form.Set("firstName", "Ada")
	_, err := testClient(srv).Do(context.Background(), Request{
		Method: "POST", Path: "/person/create.json", Form: form, Risk: manifest.RiskWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type = %q", gotCT)
	}
	if gotBody != "firstName=Ada" {
		t.Fatalf("body = %q", gotBody)
	}
}

func TestSuccessFalseIsValidationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":false,"errors":[{"field":"lastName","message":"Wert erforderlich."}]}`))
	}))
	defer srv.Close()
	_, err := testClient(srv).Do(context.Background(), Request{
		Method: "POST", Path: "/person/create.json", Form: url.Values{}, Risk: manifest.RiskWrite,
	})
	e := assertKind(t, err, KindValidation, "success=false")
	if e.Status != 200 {
		t.Errorf("status = %d, want 200", e.Status)
	}
	if !strings.Contains(e.Message, "lastName") {
		t.Errorf("message does not name the field: %q", e.Message)
	}
	if e.Details == nil {
		t.Error("details lost the server body")
	}
}

func TestSuccessTrueAndPlainDataPassThrough(t *testing.T) {
	for _, body := range []string{
		`{"success":true,"insertId":7}`,
		`{"total":1,"data":[{"id":1}]}`,
		`[1,2,3]`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(body))
		}))
		resp, err := testClient(srv).Do(context.Background(), get("/x.json"))
		srv.Close()
		if err != nil {
			t.Fatalf("body %s: %v", body, err)
		}
		if string(resp.Body) != body {
			t.Fatalf("body %s came back as %s", body, resp.Body)
		}
	}
}

func Test429RetriedWithinBudget(t *testing.T) {
	tries := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tries++
		if tries < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	var slept time.Duration
	c := testClient(srv)
	c.Sleep = func(d time.Duration) { slept += d }
	if _, err := c.Do(context.Background(), get("/x.json")); err != nil {
		t.Fatal(err)
	}
	if tries != 3 {
		t.Fatalf("tries = %d", tries)
	}
	if slept < 2*time.Second {
		t.Fatalf("slept %s, want at least the two Retry-After waits", slept)
	}
}

func Test429BudgetExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	c := testClient(srv)
	c.RetryBudget = time.Second
	_, err := c.Do(context.Background(), get("/x.json"))
	assertKind(t, err, KindRateLimited, "budget")
}

func TestGet5xxRetriedPostNot(t *testing.T) {
	tries := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tries++
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := testClient(srv)
	_, err := c.Do(context.Background(), get("/x.json"))
	assertKind(t, err, KindServer, "HTTP 500")
	if tries != 3 {
		t.Fatalf("GET tries = %d, want 3", tries)
	}

	tries = 0
	_, err = c.Do(context.Background(), Request{Method: "POST", Path: "/x.json", Form: url.Values{}, Risk: manifest.RiskWrite})
	assertKind(t, err, KindServer, "HTTP 500")
	if tries != 1 {
		t.Fatalf("POST tries = %d, want 1 — mutations are never replayed", tries)
	}
}

func TestGetFollowsRedirectPostDoesNot(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	// The redirect target says "localhost" where the server is dialed as
	// "127.0.0.1": same loopback socket, different hostname — which is what
	// makes Go treat it as a cross-host redirect and strip the Authorization
	// header, exactly as it does when CashCtrl redirects a download to its
	// storage provider.
	crossHost := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	mux.HandleFunc("/api/v1/file/get", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, crossHost+"/storage/blob", http.StatusFound)
	})
	mux.HandleFunc("/storage/blob", func(w http.ResponseWriter, r *http.Request) {
		// The presigned target must not receive the API credential.
		if r.Header.Get("Authorization") != "" {
			t.Error("Authorization header followed the cross-host redirect")
		}
		w.Write([]byte("FILEBYTES"))
	})
	mux.HandleFunc("/api/v1/move.json", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/elsewhere", http.StatusTemporaryRedirect)
	})

	c := testClient(srv)
	resp, err := c.Do(context.Background(), get("/file/get"))
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "FILEBYTES" {
		t.Fatalf("body = %q", resp.Body)
	}

	_, err = c.Do(context.Background(), Request{Method: "POST", Path: "/move.json", Form: url.Values{}, Risk: manifest.RiskWrite})
	assertKind(t, err, KindValidation, "redirect")
}

func TestForbiddenNamesTheRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer srv.Close()
	_, err := testClient(srv).Do(context.Background(), get("/x.json"))
	assertKind(t, err, KindForbidden, "role")
}

func TestPutFileRefusesPlainHTTPNonLoopback(t *testing.T) {
	c := &Client{APIKey: "k"}
	err := c.PutFile(context.Background(), "http://storage.example.com/x", "text/plain", []byte("x"))
	assertKind(t, err, KindUsage, "HTTPS")
}

func TestPutFileUploads(t *testing.T) {
	var gotBody, gotCT string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := &Client{APIKey: "secret"}
	if err := c.PutFile(context.Background(), srv.URL+"/blob", "text/plain", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if gotBody != "hello" || gotCT != "text/plain" {
		t.Fatalf("got %q %q", gotBody, gotCT)
	}
	if gotAuth != "" {
		t.Fatal("PutFile sent a credential to the storage host")
	}
}

// assertKind fails unless err is an *Error of the given kind whose message
// contains want, and returns it.
func assertKind(t *testing.T, err error, kind, want string) *Error {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want kind %s", kind)
	}
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("error %T is not *Error: %v", err, err)
	}
	if e.Kind != kind {
		t.Fatalf("kind = %q, want %q (message %q)", e.Kind, kind, e.Message)
	}
	if !strings.Contains(e.Message, want) {
		t.Fatalf("message %q does not contain %q", e.Message, want)
	}
	return e
}
