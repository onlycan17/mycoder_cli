package vectorstore

import "context"

// Noop is a local fallback that disables vector search gracefully.
type Noop struct{}

func (Noop) Upsert(ctx context.Context, items []UpsertItem) error { return nil }
func (Noop) Search(ctx context.Context, projectID string, query []float32, k int) ([]Result, error) {
	return nil, nil
}
func (Noop) DeleteByDoc(ctx context.Context, projectID, docID string) error { return nil }
