package embedpipe

import (
	"context"
	"os"

	"mycoder/internal/llm"
	"mycoder/internal/vectorstore"
)

type item struct {
	projectID string
	docID     string
	path      string
	text      string
	dim       int
	provider  string
	model     string
}

type Pipeline struct {
	emb   llm.Embedder
	vs    vectorstore.VectorStore
	model string
	batch int
	cache map[string]struct{}
	items []item
}

func New(emb llm.Embedder, vs vectorstore.VectorStore) *Pipeline {
	if emb == nil || vs == nil {
		return nil
	}
	m := os.Getenv("MYCODER_EMBEDDING_MODEL")
	if m == "" {
		m = "text-embedding-3-small"
	}
	return &Pipeline{emb: emb, vs: vs, model: m, batch: 8, cache: make(map[string]struct{})}
}

// Add schedules a document text for embedding. shaKey is used for simple de-dup.
func (p *Pipeline) Add(projectID, docID, path, sha, text string) {
	if p == nil {
		return
	}
	// sha-based cache: skip if seen
	key := projectID + "|" + path + "|" + sha
	if sha != "" {
		if _, ok := p.cache[key]; ok {
			return
		}
		p.cache[key] = struct{}{}
	}
	// truncate overly long text to a conservative size
	if len(text) > 8000 {
		text = text[:8000]
	}
	p.items = append(p.items, item{projectID: projectID, docID: docID, path: path, text: text, model: p.model})
	if len(p.items) >= p.batch {
		_ = p.Flush(context.Background())
	}
}

// Flush embeds pending items and upserts to the vector store. Retries once on failure.
func (p *Pipeline) Flush(ctx context.Context) error {
	if p == nil || len(p.items) == 0 {
		return nil
	}
	// prepare batch texts
	texts := make([]string, len(p.items))
	for i := range p.items {
		texts[i] = p.items[i].text
	}
	vecs, err := p.emb.Embeddings(ctx, p.model, texts)
	if err != nil || len(vecs) != len(texts) {
		// naive retry per item
		for i, it := range p.items {
			v, e := p.emb.Embeddings(ctx, p.model, []string{it.text})
			if e != nil || len(v) == 0 {
				continue
			}
			_ = p.vs.Upsert(ctx, []vectorstore.UpsertItem{{ProjectID: it.projectID, DocID: it.path, ChunkID: it.docID, Vector: v[0], Dim: len(v[0]), Provider: "openai", Model: p.model}})
			_ = i // no-op
		}
		p.items = p.items[:0]
		return nil
	}
	// build upsert items
	ups := make([]vectorstore.UpsertItem, 0, len(vecs))
	for i, it := range p.items {
		ups = append(ups, vectorstore.UpsertItem{ProjectID: it.projectID, DocID: it.path, ChunkID: it.docID, Vector: vecs[i], Dim: len(vecs[i]), Provider: "openai", Model: p.model})
	}
	_ = p.vs.Upsert(ctx, ups)
	p.items = p.items[:0]
	return nil
}
