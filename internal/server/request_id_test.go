package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"mycoder/internal/store"
)

func TestRequestIDGeneratedWhenMissing(t *testing.T) {
	api := NewAPI(store.New(), nil)
	mux := api.mux()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	logMiddleware(mux).ServeHTTP(rr, req)

	got := rr.Header().Get("X-Request-ID")
	if got == "" {
		t.Fatalf("expected X-Request-ID to be set")
	}
	if len(got) < 8 { // hex(12 bytes)=24, but just ensure non-trivial length
		t.Fatalf("expected non-trivial request id, got %q", got)
	}
}

func TestRequestIDPropagatesFromClient(t *testing.T) {
	api := NewAPI(store.New(), nil)
	mux := api.mux()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "abc123")
	rr := httptest.NewRecorder()

	logMiddleware(mux).ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Request-ID"); got != "abc123" {
		t.Fatalf("expected X-Request-ID to propagate, want abc123 got %q", got)
	}
}
