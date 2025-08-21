package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"mycoder/internal/store"
)

func TestShellExecPolicyBlocksCommand(t *testing.T) {
	os.Setenv("MYCODER_SHELL_DENY_REGEX", `(?i)rm\s+-rf`)
	defer os.Unsetenv("MYCODER_SHELL_DENY_REGEX")
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", t.TempDir(), nil)
	mux := api.mux()
	// non-streaming should return 403
	body := map[string]any{"projectID": p.ID, "cmd": "rm", "args": []string{"-rf", "/"}, "timeoutSec": 1}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/shell/exec", bytes.NewReader(b)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestShellExecStreamPolicyBlocks(t *testing.T) {
	os.Setenv("MYCODER_SHELL_DENY_REGEX", `(?i)rm\s+-rf`)
	defer os.Unsetenv("MYCODER_SHELL_DENY_REGEX")
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", t.TempDir(), nil)
	mux := api.mux()
	body := map[string]any{"projectID": p.ID, "cmd": "rm", "args": []string{"-rf", "/"}}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/shell/exec/stream", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d", rr.Code)
	}
	out := rr.Body.String()
	if !strings.Contains(out, "event: error") {
		t.Fatalf("expected error event")
	}
	if !strings.Contains(out, "event: exit") {
		t.Fatalf("expected exit event")
	}
}
