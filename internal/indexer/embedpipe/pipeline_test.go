package embedpipe

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"mycoder/internal/vectorstore"
)

type fakeEmb struct{ calls []string }

func (f *fakeEmb) Embeddings(ctx context.Context, model string, texts []string) ([][]float32, error) {
	f.calls = append(f.calls, model+":"+join(texts))
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 2, 3}
	}
	return out, nil
}

type fakeVS struct{ upserts [][]vectorstore.UpsertItem }

func (f *fakeVS) Upsert(ctx context.Context, items []vectorstore.UpsertItem) error {
	// copy to avoid mutation
	cp := make([]vectorstore.UpsertItem, len(items))
	copy(cp, items)
	f.upserts = append(f.upserts, cp)
	return nil
}

func (f *fakeVS) Search(ctx context.Context, projectID string, query []float32, k int) ([]vectorstore.Result, error) {
	return nil, nil
}

func (f *fakeVS) DeleteByDoc(ctx context.Context, projectID, docID string) error { return nil }

func join(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

func TestPipelineGroupsByModelAndProvider(t *testing.T) {
	oldM := os.Getenv("MYCODER_EMBEDDING_MODEL")
	oldMC := os.Getenv("MYCODER_EMBEDDING_MODEL_CODE")
	oldP := os.Getenv("MYCODER_EMBEDDING_PROVIDER")
	oldPC := os.Getenv("MYCODER_EMBEDDING_PROVIDER_CODE")
	t.Cleanup(func() {
		_ = os.Setenv("MYCODER_EMBEDDING_MODEL", oldM)
		_ = os.Setenv("MYCODER_EMBEDDING_MODEL_CODE", oldMC)
		_ = os.Setenv("MYCODER_EMBEDDING_PROVIDER", oldP)
		_ = os.Setenv("MYCODER_EMBEDDING_PROVIDER_CODE", oldPC)
	})
	_ = os.Setenv("MYCODER_EMBEDDING_MODEL", "text-model")
	_ = os.Setenv("MYCODER_EMBEDDING_MODEL_CODE", "code-model")
	_ = os.Setenv("MYCODER_EMBEDDING_PROVIDER", "prov-text")
	_ = os.Setenv("MYCODER_EMBEDDING_PROVIDER_CODE", "prov-code")

	fe := &fakeEmb{}
	fvs := &fakeVS{}
	p := New(fe, fvs)
	if p == nil {
		t.Fatalf("pipeline nil")
	}

	p.Add("proj", "doc1", "file.go", "sha1", "code content")
	p.Add("proj", "doc2", "README.md", "sha2", "text content")
	if err := p.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}
	// expect two embedding calls, one per model
	if len(fe.calls) != 2 {
		t.Fatalf("expected 2 embedding calls, got %d: %+v", len(fe.calls), fe.calls)
	}
	// verify providers propagated into upserts
	found := map[string]bool{}
	for _, batch := range fvs.upserts {
		for _, it := range batch {
			found[it.Provider+"|"+it.Model] = true
		}
	}
	want := map[string]bool{"prov-code|code-model": true, "prov-text|text-model": true}
	if !reflect.DeepEqual(found, want) {
		t.Fatalf("provider/model labels mismatch. got=%v want=%v", found, want)
	}
}

type fakeTr struct{}

func (fakeTr) Translate(ctx context.Context, srcLang, tgtLang, text string) (string, error) {
	return "[EN] " + text, nil
}

func TestTranslateFallbackKorean(t *testing.T) {
	oldF := os.Getenv("MYCODER_EMBED_TRANSLATE_FALLBACK")
	oldTO := os.Getenv("MYCODER_EMBED_TRANSLATE_TIMEOUT_MS")
	t.Cleanup(func() {
		_ = os.Setenv("MYCODER_EMBED_TRANSLATE_FALLBACK", oldF)
		_ = os.Setenv("MYCODER_EMBED_TRANSLATE_TIMEOUT_MS", oldTO)
	})
	_ = os.Setenv("MYCODER_EMBED_TRANSLATE_FALLBACK", "1")
	_ = os.Setenv("MYCODER_EMBED_TRANSLATE_TIMEOUT_MS", "500")

	fe := &fakeEmb{}
	fvs := &fakeVS{}
	p := New(fe, fvs).WithTranslator(fakeTr{})
	if p == nil {
		t.Fatalf("pipeline nil")
	}

	p.Add("proj", "d1", "README.md", "s1", "안녕하세요 세계")    // Korean
	p.Add("proj", "d2", "README.md", "s2", "hello world") // English
	_ = p.Flush(context.Background())

	// Expect that one of the embedding calls contains translated marker
	found := false
	for _, c := range fe.calls {
		if strings.Contains(c, "[EN] ") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected translated text to be embedded, calls=%v", fe.calls)
	}
}
