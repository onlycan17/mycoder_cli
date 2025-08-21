package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mycoder/internal/llm"
	"mycoder/internal/store"
)

func TestChatStreamEmitsTokenAndDone(t *testing.T) {
	// mock provider that emits two tokens then done
	prov := &mockChatProvider{chatFn: func(ctx context.Context, model string, messages []llm.Message, stream bool, temperature float32) (llm.ChatStream, error) {
		i := 0
		return &mockChatStream{RecvFn: func() (string, bool, error) {
			if i == 0 {
				i++
				return "Hello ", false, nil
			}
			if i == 1 {
				i++
				return "world", false, nil
			}
			return "", true, nil
		}}, nil
	}}
	st := store.New()
	api := NewAPI(st, prov)
	mux := api.mux()
	body := map[string]any{"messages": []map[string]any{{"role": "user", "content": "hi"}}, "stream": true}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d", rr.Code)
	}
	out := rr.Body.String()
	if !strings.Contains(out, "event: token") {
		t.Fatalf("missing token event: %q", out)
	}
	if !strings.Contains(out, "event: done") {
		t.Fatalf("missing done event: %q", out)
	}
}

func TestChatStreamEmitsError(t *testing.T) {
	prov := &mockChatProvider{chatFn: func(ctx context.Context, model string, messages []llm.Message, stream bool, temperature float32) (llm.ChatStream, error) {
		return &mockChatStream{RecvFn: func() (string, bool, error) { return "", false, context.DeadlineExceeded }}, nil
	}}
	st := store.New()
	api := NewAPI(st, prov)
	mux := api.mux()
	body := map[string]any{"messages": []map[string]any{{"role": "user", "content": "hi"}}, "stream": true}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d", rr.Code)
	}
	out := rr.Body.String()
	if !strings.Contains(out, "event: error") {
		t.Fatalf("missing error event: %q", out)
	}
}
