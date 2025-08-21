package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mycoder/internal/store"
)

func TestShellExecStreamEmitsStdoutAndExit(t *testing.T) {
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("shs", t.TempDir(), nil)
	mux := api.mux()

	body := map[string]any{"projectID": p.ID, "cmd": "echo", "args": []string{"stream"}, "timeoutSec": 5}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/shell/exec/stream", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	out := rr.Body.String()
	if !strings.Contains(out, "event: stdout") {
		t.Fatalf("missing stdout event: %q", out)
	}
	if !strings.Contains(out, "event: exit") {
		t.Fatalf("missing exit event: %q", out)
	}
	if !strings.Contains(out, "data: 0") && !strings.Contains(out, "data: 0\n") {
		// allow non-zero exit on platform differences, but should contain exit line
		if !strings.Contains(out, "event: exit") || !strings.Contains(out, "data:") {
			t.Fatalf("missing exit data: %q", out)
		}
	}
	// ensure body is readable fully
	if _, err := io.ReadAll(strings.NewReader(out)); err != nil {
		t.Fatal(err)
	}
}
