package retriever

import (
	"context"
	"os"
	"sort"
	"strconv"
)

// HybridRetriever unions BM25 and KNN results and re-ranks with a simple weighted sum.
// score = bm25 + alpha * knn
type HybridRetriever struct {
	lexical Retriever
	knn     Retriever
	alpha   float64
}

func NewHybrid(lex Retriever, knn Retriever) *HybridRetriever {
	a := 0.5
	if v := os.Getenv("MYCODER_HYBRID_ALPHA"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			a = f
		}
	}
	return &HybridRetriever{lexical: lex, knn: knn, alpha: a}
}

func (h *HybridRetriever) Retrieve(ctx context.Context, projectID string, query string, k int) ([]Result, error) {
	// fetch from both (sequential; can be parallelized later)
	lex, err := h.lexical.Retrieve(ctx, projectID, query, k)
	if err != nil {
		return nil, err
	}
	knn, err := h.knn.Retrieve(ctx, projectID, query, k)
	if err != nil {
		return nil, err
	}
	// merge by path with weighted score
	type agg struct {
		res   Result
		score float64
	}
	m := make(map[string]*agg)
	add := func(arr []Result, weight float64) {
		for _, r := range arr {
			a, ok := m[r.Path]
			if !ok {
				a = &agg{res: r}
				m[r.Path] = a
			}
			a.score += weight * r.Score
			if r.StartLine > 0 && (a.res.StartLine == 0 || r.Score > a.res.Score) {
				// prefer better-scored range for preview
				a.res.StartLine, a.res.EndLine, a.res.Preview = r.StartLine, r.EndLine, r.Preview
			}
			if r.Score > a.res.Score {
				a.res.Score = r.Score
			}
		}
	}
	add(lex, 1.0)
	add(knn, h.alpha)
	// collect and sort by aggregated score desc
	out := make([]*agg, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].score > out[j].score })
	// trim to k
	n := k
	if n <= 0 || n > len(out) {
		n = len(out)
	}
	res := make([]Result, 0, n)
	for i := 0; i < n; i++ {
		res = append(res, out[i].res)
	}
	return res, nil
}
