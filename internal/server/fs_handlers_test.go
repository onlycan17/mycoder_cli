package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"mycoder/internal/store"
)

func TestFSReadWriteDelete(t *testing.T) {
	dir := t.TempDir()
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("fs", dir, nil)
	mux := api.mux()

	// write
	wbody := map[string]any{"projectID": p.ID, "path": "a.txt", "content": "hello"}
	b, _ := json.Marshal(wbody)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/write", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("write code=%d", rr.Code)
	}
	// read
	rbody := map[string]any{"projectID": p.ID, "path": "a.txt"}
	b, _ = json.Marshal(rbody)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/read", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("read code=%d", rr.Code)
	}
	var res map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res["content"].(string) != "hello" {
		t.Fatalf("unexpected content: %v", res["content"])
	}
	// delete
	dbody := map[string]any{"projectID": p.ID, "path": "a.txt"}
	b, _ = json.Marshal(dbody)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/delete", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("delete code=%d", rr.Code)
	}
}

func TestFSPatchFlow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(path, []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("fs", dir, nil)
	mux := api.mux()
	// patch: replace cd with XY at start=2 length=2
	body := map[string]any{"projectID": p.ID, "path": "b.txt", "hunks": []map[string]any{{"start": 2, "length": 2, "replace": "XY"}}}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/patch", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("patch code=%d", rr.Code)
	}
	// read back
	rb := map[string]any{"projectID": p.ID, "path": "b.txt"}
	b, _ = json.Marshal(rb)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/read", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("read code=%d", rr.Code)
	}
	var res map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res["content"].(string) != "abXYef" {
		t.Fatalf("unexpected content after patch: %v", res["content"])
	}
}

func TestFSPathOutsideProjectForbidden(t *testing.T) {
	dir := t.TempDir()
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("fs", dir, nil)
	mux := api.mux()
	// attempt to read outside with ..
	body := map[string]any{"projectID": p.ID, "path": "../x"}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/read", bytes.NewReader(b)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}
