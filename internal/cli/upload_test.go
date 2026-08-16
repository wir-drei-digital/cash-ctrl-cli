package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFileUploadComposite(t *testing.T) {
	var putBody []byte
	var persistIDs string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/api/v1/file/prepare.json", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var files []map[string]any
		if err := json.Unmarshal([]byte(r.PostForm.Get("files")), &files); err != nil || len(files) != 1 {
			t.Errorf("prepare files = %q", r.PostForm.Get("files"))
		}
		if files[0]["name"] != "note.txt" || files[0]["mimeType"] != "text/plain; charset=utf-8" {
			t.Errorf("prepare meta = %v", files[0])
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"success":true,"data":[{"fileId":24,"writeUrl":"%s/storage/blob"}]}`, srv.URL)
	})
	mux.HandleFunc("/storage/blob", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("storage saw %s", r.Method)
		}
		putBody = make([]byte, r.ContentLength)
		r.Body.Read(putBody)
		w.WriteHeader(200)
	})
	mux.HandleFunc("/api/v1/file/persist.json", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		persistIDs = r.PostForm.Get("ids")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true}`))
	})

	path := filepath.Join(t.TempDir(), "note.txt")
	os.WriteFile(path, []byte("hello upload"), 0o644)

	ta := newTestApp(t, srv)
	code := ta.run([]string{"file", "upload", path})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, ta.errOut.String())
	}
	if string(putBody) != "hello upload" {
		t.Fatalf("storage got %q", putBody)
	}
	if persistIDs != "24" {
		t.Fatalf("persist ids = %q", persistIDs)
	}
	var out struct {
		FileID   int64  `json:"file_id"`
		Name     string `json:"name"`
		MimeType string `json:"mime_type"`
	}
	if err := json.Unmarshal(ta.out.Bytes(), &out); err != nil {
		t.Fatalf("stdout not JSON: %q", ta.out.String())
	}
	if out.FileID != 24 || out.Name != "note.txt" {
		t.Fatalf("out = %+v", out)
	}
}

func TestFileUploadBlockedByReadOnly(t *testing.T) {
	srv, _ := captureServer(t, `{}`)
	ta := newTestApp(t, srv)
	ta.client.ReadOnly = true
	path := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(path, []byte("x"), 0o644)
	code := ta.run([]string{"file", "upload", path})
	if code != 2 {
		t.Fatalf("exit %d, want 2 (stderr %s)", code, ta.errOut.String())
	}
}

func TestFileUploadMissingFile(t *testing.T) {
	ta := newTestApp(t, nil)
	if code := ta.run([]string{"file", "upload", "/does/not/exist"}); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
