package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mycoder/internal/store"
)

func TestWebSearchMockAndIngest(t *testing.T) {
	t.Setenv("MYCODER_WEB_SEARCH_MOCK", "1")
	st := store.New()
	api := NewAPI(st, nil)
	mux := api.mux()
	// search
	b, _ := json.Marshal(map[string]any{"query": "golang", "limit": 3})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/web/search", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("/web/search code=%d body=%s", rr.Code, rr.Body.String())
	}
	var res struct{ Results []map[string]any }
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if len(res.Results) != 3 {
		t.Fatalf("expected 3 results")
	}
	// create project and ingest
	p := st.CreateProject("web", "/tmp/web", nil)
	ing := map[string]any{"projectID": p.ID, "results": res.Results, "dedupe": true, "minScore": 0.0}
	ib, _ := json.Marshal(ing)
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, httptest.NewRequest(http.MethodPost, "/web/ingest", bytes.NewReader(ib)))
	if rr2.Code != http.StatusOK {
		t.Fatalf("/web/ingest code=%d body=%s", rr2.Code, rr2.Body.String())
	}
	var ir map[string]int
	_ = json.Unmarshal(rr2.Body.Bytes(), &ir)
	if ir["added"] != 3 {
		t.Fatalf("expected 3 added, got %v", ir)
	}
}
