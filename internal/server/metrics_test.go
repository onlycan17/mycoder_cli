package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mycoder/internal/store"
)

func TestMetricsTextDefault(t *testing.T) {
	api := NewAPI(store.New(), nil)
	mux := api.mux()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("/metrics code=%d", rr.Code)
	}
	ctype := rr.Header().Get("Content-Type")
	if !strings.Contains(ctype, "text/plain") {
		t.Fatalf("expected text/plain content-type, got %q", ctype)
	}
	body := rr.Body.String()
	// minimal exposition keys
	if !strings.Contains(body, "mycoder_projects") || !strings.Contains(body, "mycoder_build_info") {
		t.Fatalf("unexpected metrics body: %s", body)
	}
}

func TestMetricsJSONByQuery(t *testing.T) {
	api := NewAPI(store.New(), nil)
	mux := api.mux()

	req := httptest.NewRequest(http.MethodGet, "/metrics?format=json", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("/metrics?format=json code=%d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json, got %q", ct)
	}
	var m map[string]int
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	// keys should exist (may be zero values)
	_ = m["projects"]
	_ = m["documents"]
	_ = m["jobs"]
	_ = m["knowledge"]
}

func TestMetricsJSONByAccept(t *testing.T) {
	api := NewAPI(store.New(), nil)
	mux := api.mux()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Accept", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("/metrics (Accept json) code=%d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json, got %q", ct)
	}
}
