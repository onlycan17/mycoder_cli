package retriever

import (
	"context"
	"mycoder/internal/models"
	"testing"
)

type fakeRet struct {
	out []Result
	err error
}

func (f fakeRet) Retrieve(ctx context.Context, projectID, query string, k int) ([]Result, error) {
	return f.out, f.err
}

func TestHybridRetrieverUnionAndRank(t *testing.T) {
	bm := fakeRet{out: []Result{{Path: "a.txt", Score: 1.0}, {Path: "b.txt", Score: 0.5}}}
	kn := fakeRet{out: []Result{{Path: "a.txt", Score: 0.9}, {Path: "c.txt", Score: 0.8}}}
	h := NewHybrid(bm, kn)
	got, err := h.Retrieve(context.Background(), "p", "q", 10)
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	// expect union {a,b,c} with a first due to highest agg score
	if len(got) != 3 || got[0].Path != "a.txt" {
		t.Fatalf("unexpected results: %+v", got)
	}
	// ensure type alias behaves
	_ = models.SearchResult(got[0])
}
