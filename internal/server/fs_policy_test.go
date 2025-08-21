package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"mycoder/internal/store"
)

func TestFSPolicyBlocksWrite(t *testing.T) {
	os.Setenv("MYCODER_FS_DENY_REGEX", `(?i)^danger\.txt$`)
	defer os.Unsetenv("MYCODER_FS_DENY_REGEX")
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", t.TempDir(), nil)
	mux := api.mux()
	body := map[string]any{"projectID": p.ID, "path": "danger.txt", "content": "x"}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/write", bytes.NewReader(b)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}
