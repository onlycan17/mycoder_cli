package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"mycoder/internal/llm"
)

func TestChatRetriesOn429(t *testing.T) {
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt32(&calls, 1)
		if c < 3 {
			w.WriteHeader(429)
			w.Write([]byte("rate limit"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	os.Setenv("MYCODER_OPENAI_BASE_URL", srv.URL+"/v1")
	os.Setenv("MYCODER_LLM_MIN_INTERVAL_MS", "1")
	defer func() { os.Unsetenv("MYCODER_OPENAI_BASE_URL"); os.Unsetenv("MYCODER_LLM_MIN_INTERVAL_MS") }()

	c := NewFromEnv()
	st, err := c.Chat(context.Background(), "m", []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s, done, err := st.Recv()
	if err != nil || done || s != "ok" {
		t.Fatalf("unexpected: %q done=%v err=%v", s, done, err)
	}
}
