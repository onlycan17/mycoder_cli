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
