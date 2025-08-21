package retriever

import (
	"context"
)

// BM25Retriever delegates to an underlying lexical searcher (FTS5/BM25).
type BM25Retriever struct {
	s LexicalSearcher
}

func NewBM25(s LexicalSearcher) *BM25Retriever { return &BM25Retriever{s: s} }

func (r *BM25Retriever) Retrieve(ctx context.Context, projectID string, query string, k int) ([]Result, error) {
	// ctx reserved for future store methods supporting context.
	_ = ctx
	return r.s.Search(projectID, query, k), nil
}
