package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"mycoder/internal/llm"
)

func TestChatNonStreaming(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "hello"}}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	os.Setenv("MYCODER_OPENAI_BASE_URL", srv.URL+"/v1")
	defer os.Unsetenv("MYCODER_OPENAI_BASE_URL")
	c := NewFromEnv()
	st, err := c.Chat(context.Background(), "dummy", []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s, done, err := st.Recv()
	if err != nil || done {
		t.Fatalf("unexpected: %q done=%v err=%v", s, done, err)
	}
	if s != "hello" {
		t.Fatalf("got %q", s)
	}
}

func TestEmbeddings(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"embedding": []float32{0.1, 0.2}}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	os.Setenv("MYCODER_OPENAI_BASE_URL", srv.URL+"/v1")
	defer os.Unsetenv("MYCODER_OPENAI_BASE_URL")
	c := NewFromEnv()
	vecs, err := c.Embeddings(context.Background(), "embed", []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 2 {
		t.Fatalf("unexpected embedding size: %v", vecs)
	}
}
