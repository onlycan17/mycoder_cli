package retriever

import (
	"context"
	"mycoder/internal/llm"
	"mycoder/internal/vectorstore"
	"os"
)

// KNNRetriever uses a VectorStore and an Embedder to perform semantic search.
type KNNRetriever struct {
	vs    vectorstore.VectorStore
	emb   llm.Embedder
	model string
}

func NewKNN(vs vectorstore.VectorStore, emb llm.Embedder) *KNNRetriever {
	model := os.Getenv("MYCODER_EMBEDDING_MODEL")
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &KNNRetriever{vs: vs, emb: emb, model: model}
}

func (r *KNNRetriever) Retrieve(ctx context.Context, projectID string, query string, k int) ([]Result, error) {
	vecs, err := r.emb.Embeddings(ctx, r.model, []string{query})
	if err != nil || len(vecs) == 0 {
		// graceful fallback: no semantic results when embeddings unavailable
		return nil, nil
	}
	res, err := r.vs.Search(ctx, projectID, vecs[0], k)
	if err != nil {
		return nil, err
	}
	// adapt to []Result type alias
	out := make([]Result, len(res))
	for i := range res {
		out[i] = Result{Path: res[i].DocID, Score: res[i].Score}
	}
	return out, nil
}
