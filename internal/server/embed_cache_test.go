package server

import (
	"context"
	"mycoder/internal/llm"
	"os"
	"testing"
)

type fakeEmbedder struct{ calls int }

func (f *fakeEmbedder) Embeddings(ctx context.Context, model string, inputs []string) ([][]float32, error) {
	f.calls++
	out := make([][]float32, len(inputs))
	for i := range inputs {
		out[i] = []float32{0.1, 0.2}
	}
	return out, nil
}

func TestCachingEmbedder_HitAndTTL(t *testing.T) {
	os.Setenv("MYCODER_EMBED_CACHE_TTL_SEC", "3600")
	defer os.Unsetenv("MYCODER_EMBED_CACHE_TTL_SEC")
	fe := &fakeEmbedder{}
	ce := newCachingEmbedder(fe).(llm.Embedder)
	// first call: miss
	v1, err := ce.Embeddings(context.Background(), "m", []string{"hello"})
	if err != nil || len(v1) != 1 {
		t.Fatalf("embeddings err: %v", err)
	}
	// second call with same input: hit (no additional call)
	v2, err := ce.Embeddings(context.Background(), "m", []string{"hello"})
	if err != nil || len(v2) != 1 {
		t.Fatalf("embeddings err2: %v", err)
	}
	if fe.calls != 1 {
		t.Fatalf("expected 1 underlying call, got %d", fe.calls)
	}
}

func TestCachingEmbedder_InvalidateByGen(t *testing.T) {
	os.Setenv("MYCODER_EMBED_CACHE_TTL_SEC", "3600")
	os.Setenv("MYCODER_EMBED_CACHE_GEN", "1")
	defer os.Unsetenv("MYCODER_EMBED_CACHE_TTL_SEC")
	defer os.Unsetenv("MYCODER_EMBED_CACHE_GEN")
	fe := &fakeEmbedder{}
	ce := newCachingEmbedder(fe).(llm.Embedder)
	// first call caches under gen=1
	_, err := ce.Embeddings(context.Background(), "m", []string{"hello"})
	if err != nil {
		t.Fatalf("embeddings err: %v", err)
	}
	if fe.calls != 1 {
		t.Fatalf("expected 1 underlying call, got %d", fe.calls)
	}
	// change generation -> cache should be invalidated on next call
	os.Setenv("MYCODER_EMBED_CACHE_GEN", "2")
	_, err = ce.Embeddings(context.Background(), "m", []string{"hello"})
	if err != nil {
		t.Fatalf("embeddings err2: %v", err)
	}
	if fe.calls != 2 {
		t.Fatalf("expected 2 underlying calls after gen change, got %d", fe.calls)
	}
}
