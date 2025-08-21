package vectorstore

import (
	"context"
	"errors"
)

// PGVector is a stub for a PostgreSQL + pgvector implementation.
// It compiles but returns a not-implemented error until wired.
type PGVector struct {
	DSN string
}

func (p PGVector) Upsert(ctx context.Context, items []UpsertItem) error {
	return errors.New("pgvector: upsert not implemented")
}
func (p PGVector) Search(ctx context.Context, projectID string, query []float32, k int) ([]Result, error) {
	return nil, errors.New("pgvector: search not implemented")
}
func (p PGVector) DeleteByDoc(ctx context.Context, projectID, docID string) error {
	return errors.New("pgvector: delete not implemented")
}
