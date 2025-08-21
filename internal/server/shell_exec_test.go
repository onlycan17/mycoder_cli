package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
