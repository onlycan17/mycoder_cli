package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mycoder/internal/store"
)

func TestShellExecStreamLimit(t *testing.T) {
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("shs", t.TempDir(), nil)
	mux := api.mux()

	// generate lots of output
	script := "for i in {1..20000}; do echo 0123456789; done"
	body := map[string]any{"projectID": p.ID, "cmd": "zsh", "args": []any{"-lc", script}, "timeoutSec": 10}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/shell/exec/stream", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	out := rr.Body.String()
	if !strings.Contains(out, "event: limit") {
		t.Fatalf("expected limit event, got: %q", out)
	}
	if !strings.Contains(out, "event: exit") {
		t.Fatalf("expected exit event")
	}
}
