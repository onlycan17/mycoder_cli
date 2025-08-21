package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mycoder/internal/store"
)

func TestIndexRunStreamBasic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("# doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", dir, nil)
	mux := api.mux()
	body := map[string]any{"projectID": p.ID, "mode": "full"}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/index/run/stream", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	out := rr.Body.String()
	if !strings.Contains(out, "event: job") {
		t.Fatalf("missing job event")
	}
	if !strings.Contains(out, "event: progress") {
		t.Fatalf("missing progress event")
	}
	if !strings.Contains(out, "event: completed") {
		t.Fatalf("missing completed event")
	}
}
