package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mycoder/internal/store"
)

func TestKnowledgeApprovePinsAndRaisesTrust(t *testing.T) {
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", t.TempDir(), nil)
	// add two web knowledge items (unpinned, low trust)
	k1, _ := st.AddKnowledge(p.ID, "web", "u1", "t1", "x", 0.2, false)
	k2, _ := st.AddKnowledge(p.ID, "web", "u2", "t2", "y", 0.1, false)
	mux := api.mux()
	body, _ := json.Marshal(map[string]any{"ProjectID": p.ID, "IDs": []string{k1.ID, k2.ID}, "MinTrust": 0.9})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/knowledge/approve", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	// verify via list
	list, _ := st.ListKnowledge(p.ID, 0)
	for _, k := range list {
		if k.ID == k1.ID || k.ID == k2.ID {
			if !k.Pinned || k.TrustScore < 0.9 {
				t.Fatalf("not approved: %+v", k)
			}
		}
	}
}
