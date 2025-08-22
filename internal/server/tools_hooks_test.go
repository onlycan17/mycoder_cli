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

func TestToolsHooksRunsTargets(t *testing.T) {
	dir := t.TempDir()
	// minimal Makefile with targets
	mf := "fmt-check:\n\t@echo fmt-ok\n\n" +
		"test:\n\t@echo test-ok\n\n" +
		"lint:\n\t@echo lint-ok\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mf), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("demo", dir, nil)
	mux := api.mux()

	body, _ := json.Marshal(map[string]any{"projectID": p.ID})
	req := httptest.NewRequest(http.MethodPost, "/tools/hooks", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("/tools/hooks code=%d body=%s", rr.Code, rr.Body.String())
	}
	var res map[string]struct {
		Ok     bool   `json:"ok"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !res["fmt-check"].Ok || !res["test"].Ok || !res["lint"].Ok {
		t.Fatalf("expected all ok, got: %+v", res)
	}
}

func TestToolsHooksStopsOnFailure(t *testing.T) {
	dir := t.TempDir()
	// Makefile: fmt-check ok, test fails
	mf := "fmt-check:\n\t@echo fmt-ok\n\n" +
		"test:\n\t@echo test-fail && exit 2\n\n" +
		"lint:\n\t@echo lint-ok\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mf), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("demo", dir, nil)
	mux := api.mux()

	body, _ := json.Marshal(map[string]any{"projectID": p.ID, "targets": []string{"fmt-check", "test", "lint"}})
	req := httptest.NewRequest(http.MethodPost, "/tools/hooks", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("/tools/hooks code=%d body=%s", rr.Code, rr.Body.String())
	}
	var res map[string]struct {
		Ok     bool   `json:"ok"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !res["fmt-check"].Ok {
		t.Fatalf("fmt-check should be ok, got: %+v", res["fmt-check"])
	}
	if res["test"].Ok {
		t.Fatalf("test should fail, got ok=true")
	}
	if res["test"].Output == "" {
		t.Fatalf("expected output for failed target")
	}
	// suggestion should be present
	// decode again with suggestion
	var res2 map[string]struct {
		Ok                 bool
		Output, Suggestion string
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res2)
	if res2["test"].Suggestion == "" {
		t.Fatalf("expected suggestion for failure")
	}
	if _, exists := res["lint"]; exists {
		t.Fatalf("lint should not run after failure")
	}
}

func TestToolsHooksFmtSuggestion(t *testing.T) {
	dir := t.TempDir()
	mf := "fmt-check:\n\t@echo Files need formatting: a.go\n\t@exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mf), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("demo", dir, nil)
	mux := api.mux()
	body, _ := json.Marshal(map[string]any{"projectID": p.ID, "targets": []string{"fmt-check"}})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/tools/hooks", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d", rr.Code)
	}
	var res map[string]struct {
		Ok                 bool
		Output, Suggestion string
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res["fmt-check"].Ok {
		t.Fatalf("expected fmt-check to fail")
	}
	if !strings.Contains(res["fmt-check"].Suggestion, "make fmt") {
		t.Fatalf("expected make fmt suggestion: %v", res["fmt-check"].Suggestion)
	}
}

func TestToolsHooksLintSuggestion(t *testing.T) {
	dir := t.TempDir()
	mf := "lint:\n\t@echo vet: undeclared name: X\n\t@exit 2\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mf), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("demo", dir, nil)
	mux := api.mux()
	body, _ := json.Marshal(map[string]any{"projectID": p.ID, "targets": []string{"lint"}})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/tools/hooks", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d", rr.Code)
	}
	var res map[string]struct {
		Ok                 bool
		Output, Suggestion string
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res["lint"].Ok {
		t.Fatalf("expected lint to fail")
	}
	if !strings.Contains(res["lint"].Suggestion, "go vet") {
		t.Fatalf("expected vet suggestion: %v", res["lint"].Suggestion)
	}
}

func TestToolsHooksTestPanicSuggestion(t *testing.T) {
	dir := t.TempDir()
	mf := "test:\n\t@echo panic: runtime error\n\t@exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mf), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("demo", dir, nil)
	mux := api.mux()
	body, _ := json.Marshal(map[string]any{"projectID": p.ID, "targets": []string{"test"}})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/tools/hooks", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d", rr.Code)
	}
	var res map[string]struct {
		Ok                 bool
		Suggestion, Reason string
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res["test"].Ok {
		t.Fatalf("expected test to fail")
	}
	if !strings.Contains(res["test"].Suggestion, "패닉") || res["test"].Reason != "panic" {
		t.Fatalf("expected panic suggestion/reason, got: %+v", res["test"])
	}
}

func TestToolsHooksTimeoutSuggestion(t *testing.T) {
	dir := t.TempDir()
	mf := "slow:\n\t@sleep 2\n\t@exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mf), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("demo", dir, nil)
	mux := api.mux()
	body, _ := json.Marshal(map[string]any{"projectID": p.ID, "targets": []string{"slow"}, "timeoutSec": 1})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/tools/hooks", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var res map[string]struct {
		Ok                 bool
		Suggestion, Output string
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res["slow"].Ok {
		t.Fatalf("expected slow to fail by timeout")
	}
	if !strings.Contains(res["slow"].Suggestion, "타임아웃") {
		t.Fatalf("expected timeout suggestion, got: %s", res["slow"].Suggestion)
	}
}

func TestToolsHooksModuleMissingSuggestion(t *testing.T) {
	dir := t.TempDir()
	mf := "test:\n\t@echo no required module provides package example.com/missing\n\t@exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mf), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("demo", dir, nil)
	mux := api.mux()
	body, _ := json.Marshal(map[string]any{"projectID": p.ID, "targets": []string{"test"}})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/tools/hooks", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d", rr.Code)
	}
	var res map[string]struct {
		Ok                 bool
		Suggestion, Reason string
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res["test"].Ok {
		t.Fatalf("expected test to fail")
	}
	if !strings.Contains(res["test"].Suggestion, "go mod tidy") || res["test"].Reason != "mod-missing" {
		t.Fatalf("expected module missing suggestion/reason, got: %+v", res["test"])
	}
}

func TestToolsHooksEnvWhitelist(t *testing.T) {
	dir := t.TempDir()
	mf := "show:\n\t@echo GOFLAGS=$$GOFLAGS\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mf), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("envtest", dir, nil)
	mux := api.mux()

	body, _ := json.Marshal(map[string]any{
		"projectID": p.ID,
		"targets":   []string{"show"},
		"env":       map[string]string{"GOFLAGS": "-race"},
	})
	req := httptest.NewRequest(http.MethodPost, "/tools/hooks", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("/tools/hooks code=%d body=%s", rr.Code, rr.Body.String())
	}
	var res map[string]struct {
		Ok     bool
		Output string
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(res["show"].Output, "-race") {
		t.Fatalf("expected GOFLAGS to contain -race, got: %s", res["show"].Output)
	}
}
