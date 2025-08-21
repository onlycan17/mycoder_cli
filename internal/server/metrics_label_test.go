package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"mycoder/internal/store"
)

func TestMetricsPathNormalization(t *testing.T) {
	// ensure sampling always on
	metricsSampleRate = 1.0
	st := store.New()
	api := NewAPI(st, nil)
	mux := api.mux()
	// clear metrics
	metrics.mu.Lock()
	metrics.reqTotal = make(map[string]int)
	metrics.durSum = make(map[string]float64)
	metrics.durCount = make(map[string]int)
	metrics.mu.Unlock()

	// send a request to /index/jobs/<id> which will 404 but still be recorded
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/index/jobs/abc123", nil)
	logMiddleware(mux).ServeHTTP(rr, req)

	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	found := false
	for k := range metrics.reqTotal {
		if kContains(k, "/index/jobs/:id") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected normalized key for /index/jobs/:id, got keys: %+v", keys(metrics.reqTotal))
	}
}

func kContains(k, sub string) bool { return len(k) >= len(sub) && (k == sub || contains(k, sub)) }
func contains(s, sub string) bool {
	return (len(sub) == 0) || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	// simple substring search
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
func keys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
