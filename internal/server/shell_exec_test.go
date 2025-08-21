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

func TestShellExecEcho(t *testing.T) {
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("sh", t.TempDir(), nil)
	mux := api.mux()

	body := map[string]any{"projectID": p.ID, "cmd": "echo", "args": []string{"hello"}, "timeoutSec": 5}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/shell/exec", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("exec code=%d body=%s", rr.Code, rr.Body.String())
	}
	var res struct {
		ExitCode int
		Output   string
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("unexpected exit: %d output=%s", res.ExitCode, res.Output)
	}
	if res.Output == "" {
		t.Fatalf("expected output")
	}
}

func TestShellExecWithCwdAndEnv(t *testing.T) {
	st := store.New()
	api := NewAPI(st, nil)
	root := t.TempDir()
	// create subdir
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p := st.CreateProject("sh", root, nil)
	mux := api.mux()

	body := map[string]any{
		"projectID":  p.ID,
		"cmd":        "sh",
		"args":       []string{"-lc", "pwd; echo GOFLAGS=$GOFLAGS"},
		"timeoutSec": 5,
		"cwd":        "sub",
		"env":        map[string]string{"GOFLAGS": "-race"},
	}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/shell/exec", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("exec code=%d body=%s", rr.Code, rr.Body.String())
	}
	var res struct {
		ExitCode int
		Output   string
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d out=%s", res.ExitCode, res.Output)
	}
	if !strings.Contains(res.Output, "/sub") {
		t.Fatalf("expected pwd to contain /sub: %s", res.Output)
	}
	if !strings.Contains(res.Output, "GOFLAGS=-race") {
		t.Fatalf("expected GOFLAGS in env: %s", res.Output)
	}
}

func TestShellExecOutputLimit(t *testing.T) {
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("sh", t.TempDir(), nil)
	mux := api.mux()

	// Generate large output using zsh loop
	script := "for i in {1..50000}; do echo 0123456789; done"
	body := map[string]any{"projectID": p.ID, "cmd": "zsh", "args": []string{"-lc", script}, "timeoutSec": 10}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/shell/exec", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("exec code=%d body=%s", rr.Code, rr.Body.String())
	}
	var res struct {
		ExitCode  int
		Output    string
		Truncated bool
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d", res.ExitCode)
	}
	if !strings.Contains(res.Output, "0123456789") {
		t.Fatalf("expected pattern in output")
	}
	if !res.Truncated {
		t.Fatalf("expected truncation for large output")
	}
	if len(res.Output) < 60000 {
		t.Fatalf("expected near cap length, got %d", len(res.Output))
	}
}
