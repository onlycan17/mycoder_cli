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

func TestKnowledgePromoteAutoFallback(t *testing.T) {
	dir := t.TempDir()
	// prepare files
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\nfunc X(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "y.go"), []byte("package y\nfunc Y(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	api := NewAPI(store.New(), nil) // no LLM -> fallback summary path
	mux := api.mux()

	// create project
	var pid string
	{
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewReader([]byte(`{"name":"p","rootPath":"`+dir+`"}`)))
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("create project %d", rr.Code)
		}
		var res map[string]string
		_ = json.Unmarshal(rr.Body.Bytes(), &res)
		pid = res["projectID"]
	}

	// auto-promote
	body := []byte(`{"projectID":"` + pid + `","files":["x.go","y.go"],"title":"Auto"}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/knowledge/promote/auto", bytes.NewReader(body))
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("promote-auto %d", rr.Code)
	}

	// list knowledge
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/knowledge?projectID="+pid, nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list knowledge %d", rr.Code)
	}
	var res struct {
		Knowledge []map[string]any `json:"knowledge"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if len(res.Knowledge) == 0 {
		t.Fatalf("expected promoted knowledge")
	}

	k := res.Knowledge[0]

	// Verify fallback summary was created
	text, ok := k["text"].(string)
	if !ok {
		t.Fatal("expected text field to be string")
	}

	// Should contain fallback indicator
	if !strings.Contains(text, "CodeCard: Summary of files") {
		t.Error("expected fallback summary to contain 'CodeCard: Summary of files'")
	}

	// Should contain file names
	if !strings.Contains(text, "x.go") || !strings.Contains(text, "y.go") {
		t.Error("expected summary to contain file names")
	}

	// Should contain actual file content
	if !strings.Contains(text, "package x") || !strings.Contains(text, "func X()") {
		t.Error("expected summary to contain file content")
	}

	// Verify metadata
	if k["title"] != "Auto" {
		t.Errorf("unexpected title: %v", k["title"])
	}
	if k["sourceType"] != "code" {
		t.Errorf("unexpected sourceType: %v", k["sourceType"])
	}
	if k["trustScore"] != 0.7 {
		t.Errorf("unexpected trustScore: %v", k["trustScore"])
	}
	if k["files"] != "x.go,y.go" {
		t.Errorf("unexpected files: %v", k["files"])
	}
}

func TestKnowledgePromoteAutoWithPin(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.go"), []byte("package test\n// test file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	api := NewAPI(store.New(), nil) // no LLM
	mux := api.mux()
	p := api.store.CreateProject("test", dir, nil)

	// test with pin=true
	body := []byte(`{"projectID":"` + p.ID + `","files":["test.go"],"title":"Pinned Test","pin":true}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/knowledge/promote/auto", bytes.NewReader(body))
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("promote-auto failed: %d", rr.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["pinned"] != true {
		t.Errorf("expected pinned=true, got %v", result["pinned"])
	}
}

func TestKnowledgePromoteAutoEmptyFiles(t *testing.T) {
	api := NewAPI(store.New(), nil)
	mux := api.mux()
	p := api.store.CreateProject("test", t.TempDir(), nil)

	// test with empty files array
	body := []byte(`{"projectID":"` + p.ID + `","files":[],"title":"Empty"}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/knowledge/promote/auto", bytes.NewReader(body))
	mux.ServeHTTP(rr, req)

	// should return bad request
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
