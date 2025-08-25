package retriever

import (
	"context"
)

// QueryCase bundles a query text with its ground-truth relevant paths.
type QueryCase struct {
	Query string
	Truth []string
}

// Metrics aggregates leaderboard numbers.
type Metrics struct {
	KAt5  float64
	KAt10 float64
	MRR   float64
}

// Evaluate runs a retriever across cases and computes k@5, k@10, and MRR.
func Evaluate(ctx context.Context, r Retriever, projectID string, cases []QueryCase) (Metrics, error) {
	var hits5, hits10, sumRR float64
	n := float64(len(cases))
	for _, c := range cases {
		res, err := r.Retrieve(ctx, projectID, c.Query, 10)
		if err != nil {
			return Metrics{}, err
		}
		truth := toSet(c.Truth)
		if hitAtK(res, truth, 5) {
			hits5 += 1
		}
		if hitAtK(res, truth, 10) {
			hits10 += 1
		}
		sumRR += rr(res, truth)
	}
	if n == 0 {
		return Metrics{}, nil
	}
	return Metrics{KAt5: hits5 / n, KAt10: hits10 / n, MRR: sumRR / n}, nil
}

func toSet(xs []string) map[string]struct{} {
	m := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		m[x] = struct{}{}
	}
	return m
}

func hitAtK(res []Result, truth map[string]struct{}, k int) bool {
	if k > len(res) {
		k = len(res)
	}
	for i := 0; i < k; i++ {
		if _, ok := truth[res[i].Path]; ok {
			return true
		}
	}
	return false
}

func rr(res []Result, truth map[string]struct{}) float64 {
	for i := 0; i < len(res); i++ {
		if _, ok := truth[res[i].Path]; ok {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}
