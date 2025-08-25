package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"mycoder/internal/store"
)

func TestRateLimit429AndRetryAfter(t *testing.T) {
	// enable limit: 1 RPS
	old := os.Getenv("MYCODER_RATE_LIMIT_RPS")
	t.Cleanup(func() { _ = os.Setenv("MYCODER_RATE_LIMIT_RPS", old) })
	_ = os.Setenv("MYCODER_RATE_LIMIT_RPS", "")
	_ = os.Setenv("MYCODER_RATE_LIMIT_GLOBAL_RPS", "1")

	api := NewAPI(store.New(), nil)
	mux := api.mux()
	h := logMiddleware(rateLimitMiddleware(mux))

	// same IP, two quick requests -> second should be 429
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", rr1.Code)
	}

	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request expected 429, got %d", rr2.Code)
	}
	if v := rr2.Header().Get("Retry-After"); v == "" {
		t.Fatalf("expected Retry-After header to be set")
	}
}

func TestRateLimitDisabledWhenEnvNotSet(t *testing.T) {
	old := os.Getenv("MYCODER_RATE_LIMIT_RPS")
	t.Cleanup(func() { _ = os.Setenv("MYCODER_RATE_LIMIT_RPS", old) })
	_ = os.Setenv("MYCODER_RATE_LIMIT_RPS", "")
	_ = os.Setenv("MYCODER_RATE_LIMIT_GLOBAL_RPS", "")
	_ = os.Setenv("MYCODER_RATE_LIMIT_PATH_RPS", "")
	_ = os.Setenv("MYCODER_RATE_LIMIT_IP_RPS", "")

	api := NewAPI(store.New(), nil)
	mux := api.mux()
	h := logMiddleware(rateLimitMiddleware(mux))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "198.51.100.2:34567"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with limiter disabled, got %d", rr.Code)
	}
}

func TestPathRateLimitSeparateFromGlobal(t *testing.T) {
	oldG := os.Getenv("MYCODER_RATE_LIMIT_GLOBAL_RPS")
	oldP := os.Getenv("MYCODER_RATE_LIMIT_PATH_RPS")
	t.Cleanup(func() {
		_ = os.Setenv("MYCODER_RATE_LIMIT_GLOBAL_RPS", oldG)
		_ = os.Setenv("MYCODER_RATE_LIMIT_PATH_RPS", oldP)
	})
	_ = os.Setenv("MYCODER_RATE_LIMIT_GLOBAL_RPS", "0")
	_ = os.Setenv("MYCODER_RATE_LIMIT_PATH_RPS", "1")

	api := NewAPI(store.New(), nil)
	mux := api.mux()
	h := logMiddleware(rateLimitMiddleware(mux))

	req1 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req1.RemoteAddr = "203.0.113.5:1000"
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first /a expected 200, got %d", rr1.Code)
	}

	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req1)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second /a expected 429, got %d", rr2.Code)
	}

	// different path should have its own bucket
	reqB := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	reqB.RemoteAddr = "203.0.113.5:1000"
	rrB := httptest.NewRecorder()
	h.ServeHTTP(rrB, reqB)
	if rrB.Code != http.StatusOK {
		t.Fatalf("/b expected 200, got %d", rrB.Code)
	}
}

func TestIPRateLimitPerClient(t *testing.T) {
	old := os.Getenv("MYCODER_RATE_LIMIT_IP_RPS")
	t.Cleanup(func() { _ = os.Setenv("MYCODER_RATE_LIMIT_IP_RPS", old) })
	_ = os.Setenv("MYCODER_RATE_LIMIT_IP_RPS", "1")

	api := NewAPI(store.New(), nil)
	mux := api.mux()
	h := logMiddleware(rateLimitMiddleware(mux))

	// client A
	reqA := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	reqA.RemoteAddr = "192.0.2.10:2222"
	rrA1 := httptest.NewRecorder()
	h.ServeHTTP(rrA1, reqA)
	if rrA1.Code != http.StatusOK {
		t.Fatalf("A1 expected 200, got %d", rrA1.Code)
	}
	rrA2 := httptest.NewRecorder()
	h.ServeHTTP(rrA2, reqA)
	if rrA2.Code != http.StatusTooManyRequests {
		t.Fatalf("A2 expected 429, got %d", rrA2.Code)
	}

	// client B should be independent
	reqB := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	reqB.RemoteAddr = "192.0.2.11:3333"
	rrB := httptest.NewRecorder()
	h.ServeHTTP(rrB, reqB)
	if rrB.Code != http.StatusOK {
		t.Fatalf("B expected 200, got %d", rrB.Code)
	}
}
