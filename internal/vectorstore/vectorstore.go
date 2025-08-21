package vectorstore

import (
	"context"
)

// UpsertItem represents a single embedding to store.
type UpsertItem struct {
	ProjectID string
	DocID     string
	ChunkID   string
	Vector    []float32
	Dim       int
	Provider  string
	Model     string
}

// Result represents a single nearest neighbor result.
type Result struct {
	DocID   string
	ChunkID string
	Score   float64 // higher is better similarity
}

// VectorStore defines minimal operations for semantic search.
type VectorStore interface {
	Upsert(ctx context.Context, items []UpsertItem) error
	Search(ctx context.Context, projectID string, query []float32, k int) ([]Result, error)
	DeleteByDoc(ctx context.Context, projectID, docID string) error
}
