package retriever

import (
	"context"
	"mycoder/internal/models"
	"testing"
)

type fakeSearch struct{ res []models.SearchResult }

func (f fakeSearch) Search(projectID, query string, k int) []models.SearchResult { return f.res }

func TestBM25Retriever(t *testing.T) {
	want := []models.SearchResult{{Path: "a.txt", Score: 1.23}}
	r := NewBM25(fakeSearch{res: want})
	got, err := r.Retrieve(context.Background(), "p", "q", 5)
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	if len(got) != 1 || got[0].Path != "a.txt" || got[0].Score != 1.23 {
		t.Fatalf("unexpected results: %+v", got)
	}
}
