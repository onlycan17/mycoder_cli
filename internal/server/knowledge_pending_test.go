package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"mycoder/internal/store"
)

func TestKnowledgePendingListsUnpinned(t *testing.T) {
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", t.TempDir(), nil)
	// add pinned and unpinned
	_, _ = st.AddKnowledge(p.ID, "web", "u1", "t1", "x", 0.5, false)
	_, _ = st.AddKnowledge(p.ID, "web", "u2", "t2", "y", 0.7, true)
	mux := api.mux()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/knowledge/pending?projectID="+p.ID, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if !contains(rr.Body.String(), "u1") || contains(rr.Body.String(), "u2") {
		t.Fatalf("unexpected pending list: %s", rr.Body.String())
	}
}

func hasSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (len(sub) == 0 || (len(s) >= len(sub) && (string([]byte(s)[:len(sub)]) == sub || (len(s) > 1 && contains(s[1:], sub)))))
}
