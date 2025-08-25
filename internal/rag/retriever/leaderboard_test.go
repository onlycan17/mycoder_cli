package retriever

import (
	"context"
	"fmt"
	"testing"
)

// mapRet returns pre-canned results per query.
type mapRet map[string][]Result

func (m mapRet) Retrieve(ctx context.Context, projectID, query string, k int) ([]Result, error) {
	out := m[query]
	if k > 0 && k < len(out) {
		out = out[:k]
	}
	return out, nil
}

func TestHybridAlphaLeaderboard(t *testing.T) {
	// synthetic dataset with 3 queries
	// q1: KNN strongly favors truth; q2: BM25 strong; q3: both contribute
	lex := mapRet{
		"q1": {{Path: "a", Score: 0.3}, {Path: "x", Score: 1.0}},
		"q2": {{Path: "b", Score: 1.0}, {Path: "z", Score: 0.9}},
		"q3": {{Path: "c", Score: 0.4}, {Path: "w", Score: 0.5}},
	}
	knn := mapRet{
		"q1": {{Path: "a", Score: 1.0}, {Path: "y", Score: 0.9}},
		"q2": {{Path: "b", Score: 0.2}, {Path: "t", Score: 1.0}},
		"q3": {{Path: "c", Score: 0.6}},
	}
	truth := []QueryCase{
		{Query: "q1", Truth: []string{"a"}},
		{Query: "q2", Truth: []string{"b"}},
		{Query: "q3", Truth: []string{"c"}},
	}
	alphas := []float64{0.0, 0.5, 1.0}
	var bestAlpha float64
	var bestMRR float64
	for _, a := range alphas {
		h := NewHybridWithAlpha(lex, knn, a)
		m, err := Evaluate(context.Background(), h, "p", truth)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		t.Logf("alpha=%.2f  k@5=%.2f  k@10=%.2f  MRR=%.3f", a, m.KAt5, m.KAt10, m.MRR)
		if m.MRR > bestMRR {
			bestMRR, bestAlpha = m.MRR, a
		}
	}
	// For this dataset, pure KNN wins overall
	if bestAlpha != 1.0 {
		t.Fatalf("expected best alpha=1.0, got %.2f", bestAlpha)
	}
	// Ensure all k@5 are perfect in this synthetic set
	h := NewHybridWithAlpha(lex, knn, bestAlpha)
	m, _ := Evaluate(context.Background(), h, "p", truth)
	if m.KAt5 < 1.0 {
		t.Fatalf("expected k@5=1.0, got %.2f", m.KAt5)
	}
	_ = fmt.Sprintf("") // silence import warning in some toolchains
}
