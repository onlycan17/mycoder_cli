package retriever

import (
	"context"
	"errors"
	"mycoder/internal/vectorstore"
	"testing"
)

type fakeEmbed struct{}

func (fakeEmbed) Embeddings(ctx context.Context, model string, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, errors.New("no inputs")
	}
	return [][]float32{{0.1, 0.2, 0.3}}, nil
}

type fakeVS struct{ q []float32 }

func (f *fakeVS) Upsert(ctx context.Context, items []vectorstore.UpsertItem) error { return nil }
func (f *fakeVS) Search(ctx context.Context, projectID string, query []float32, k int) ([]vectorstore.Result, error) {
	f.q = query
	return []vectorstore.Result{{DocID: "a.txt", Score: 0.9}}, nil
}
func (f *fakeVS) DeleteByDoc(ctx context.Context, projectID, docID string) error { return nil }

func TestKNNRetriever(t *testing.T) {
	vs := &fakeVS{}
	r := NewKNN(vs, fakeEmbed{})
	got, err := r.Retrieve(context.Background(), "p", "hello", 3)
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	if len(got) != 1 || got[0].Path != "a.txt" || got[0].Score <= 0 {
		t.Fatalf("unexpected results: %+v", got)
	}
	if len(vs.q) == 0 {
		t.Fatalf("expected query vector to be used")
	}
}
