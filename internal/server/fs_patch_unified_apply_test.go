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

func TestFSPatchUnifiedApplyOK(t *testing.T) {
	dir := t.TempDir()
	// seed file
	orig := "line1\nline2\n"
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", dir, nil)
	mux := api.mux()
	diff := "" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -1,2 +1,3 @@\n" +
		" line1\n" +
		"-line2\n" +
		"+line2 modified\n" +
		"+line3\n"
	body, _ := json.Marshal(map[string]any{"projectID": p.ID, "diffText": diff, "dryRun": false, "yes": true})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/patch/unified", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	out, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(out) != "line1\nline2 modified\nline3\n" {
		t.Fatalf("content mismatch: %q", string(out))
	}
}

func TestFSPatchUnifiedConflict(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", dir, nil)
	mux := api.mux()
	// diff expects first line to be 'x', which mismatches
	diff := "--- a/a.txt\n+++ b/a.txt\n@@ -1,1 +1,1 @@\n-xxx\n+yyy\n"
	body, _ := json.Marshal(map[string]any{"projectID": p.ID, "diffText": diff, "dryRun": false, "yes": true})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/patch/unified", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	// content should remain unchanged
	out, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(out) != "hello\nworld\n" {
		t.Fatalf("should not change on conflict: %q", string(out))
	}
}

func TestFSPatchUnifiedRollback(t *testing.T) {
	dir := t.TempDir()
	// seed file
	orig := "hello\nworld\n"
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", dir, nil)
	mux := api.mux()
	diff := "--- a/a.txt\n+++ b/a.txt\n@@ -1,2 +1,2 @@\n-hello\n-world\n+hello there\n+world!\n"
	// apply and capture patchID
	body, _ := json.Marshal(map[string]any{"projectID": p.ID, "diffText": diff, "dryRun": false, "yes": true})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/patch/unified", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("apply code=%d body=%s", rr.Code, rr.Body.String())
	}
	var res map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	pid, _ := res["patchID"].(string)
	if pid == "" {
		t.Fatalf("missing patchID")
	}
	// now rollback
	rb, _ := json.Marshal(map[string]any{"projectID": p.ID, "patchID": pid, "yes": true})
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, httptest.NewRequest(http.MethodPost, "/fs/patch/unified/rollback", bytes.NewReader(rb)))
	if rr2.Code != http.StatusOK {
		t.Fatalf("rollback code=%d body=%s", rr2.Code, rr2.Body.String())
	}
	out, _ := os.ReadFile(path)
	if string(out) != orig {
		t.Fatalf("rollback failed, got: %q", string(out))
	}
}
