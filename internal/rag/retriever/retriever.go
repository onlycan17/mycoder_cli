package retriever

import (
	"context"
	"mycoder/internal/models"
)

// Result is an alias to models.SearchResult for clarity at call sites.
type Result = models.SearchResult

// Retriever returns top-K context items for a query.
type Retriever interface {
	Retrieve(ctx context.Context, projectID string, query string, k int) ([]Result, error)
}

// LexicalSearcher is the minimal capability needed from a backing store.
// It mirrors the existing Store.Search(projectID, query, k).
type LexicalSearcher interface {
	Search(projectID, query string, k int) []models.SearchResult
}
