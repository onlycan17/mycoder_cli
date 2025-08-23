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

func TestAuthTokenRequired(t *testing.T) {
	t.Setenv("MYCODER_API_TOKEN", "secret")
	api := NewAPI(store.New(), nil)
	mux := api.mux()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/projects", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing token, got %d", rr.Code)
	}
	// with bearer token
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req2.Header.Set("Authorization", "Bearer secret")
	mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200 with token, got %d", rr2.Code)
	}
}

func TestReadOnlyBlocksFSWrite(t *testing.T) {
	t.Setenv("MYCODER_READONLY", "1")
	dir := t.TempDir()
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", dir, nil)
	mux := api.mux()
	body := map[string]any{"projectID": p.ID, "path": "a.txt", "content": "hi"}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/write", bytes.NewReader(b)))
	if rr.Code != http.StatusForbidden {
		// helpful output when running locally
		os.WriteFile(filepath.Join(dir, "resp.txt"), rr.Body.Bytes(), 0o644)
		t.Fatalf("expected 403 in read-only, got %d, body=%s", rr.Code, rr.Body.String())
	}
}
