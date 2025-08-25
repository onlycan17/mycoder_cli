package retriever

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"mycoder/internal/llm"
	istore "mycoder/internal/store"
	"mycoder/internal/vectorstore"
)

// bowEmb is a tiny bag-of-words embedder for tests.
type bowEmb struct{ dim int }

func (b bowEmb) Embeddings(ctx context.Context, model string, inputs []string) ([][]float32, error) {
	out := make([][]float32, len(inputs))
	for i, s := range inputs {
		v := make([]float32, b.dim)
		for _, tok := range strings.Fields(strings.ToLower(s)) {
			h := hash(tok) % uint32(b.dim)
			v[h] += 1
		}
		out[i] = v
	}
	return out, nil
}

func hash(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// memVS is an in-memory vector store keyed by docID.
type memVS struct{ items []vectorstore.UpsertItem }

func (m *memVS) Upsert(ctx context.Context, items []vectorstore.UpsertItem) error {
	m.items = append(m.items, items...)
	return nil
}
func (m *memVS) Search(ctx context.Context, projectID string, query []float32, k int) ([]vectorstore.Result, error) {
	type sc struct {
		i int
		s float64
	}
	var arr []sc
	for i := range m.items {
		v := m.items[i].Vector
		arr = append(arr, sc{i: i, s: float64(cos(query, v))})
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].s > arr[j].s })
	if k > len(arr) {
		k = len(arr)
	}
	out := make([]vectorstore.Result, 0, k)
	for i := 0; i < k; i++ {
		it := m.items[arr[i].i]
		out = append(out, vectorstore.Result{DocID: it.DocID, ChunkID: it.ChunkID, Score: arr[i].s})
	}
	return out, nil
}
func (m *memVS) DeleteByDoc(ctx context.Context, projectID, docID string) error { return nil }

type evalFile struct {
	Documents []struct{ Path, Text string } `json:"documents"`
	Cases     []QueryCase                   `json:"cases"`
}

func TestDatasetEvaluateIfProvided(t *testing.T) {
	path := os.Getenv("MYCODER_EVAL_CASES")
	if path == "" {
		t.Skip("set MYCODER_EVAL_CASES to run dataset evaluation")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cases: %v", err)
	}
	var f evalFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Cases) == 0 || len(f.Documents) == 0 {
		t.Fatalf("empty dataset")
	}

	// prepare stores
	s := istore.New()
	proj := s.CreateProject("eval", ".", nil)
	for _, d := range f.Documents {
		s.AddDocument(proj.ID, d.Path, d.Text)
	}
	// prepare knn: embed each doc as a single vector
	emb := bowEmb{dim: 64}
	mvs := &memVS{}
	for _, d := range f.Documents {
		vec, _ := emb.Embeddings(context.Background(), "bow", []string{d.Text})
		_ = mvs.Upsert(context.Background(), []vectorstore.UpsertItem{{ProjectID: proj.ID, DocID: d.Path, ChunkID: "", Vector: vec[0], Dim: len(vec[0]), Provider: "test", Model: "bow"}})
	}
	bm := NewBM25(s)
	kn := NewKNN(mvs, emb)
	h := NewHybrid(bm, kn)
	m, err := Evaluate(context.Background(), h, proj.ID, f.Cases)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	t.Logf("dataset metrics: k@5=%.2f k@10=%.2f MRR=%.3f", m.KAt5, m.KAt10, m.MRR)
	// The test passes if it runs; no hard threshold enforced here
	_ = llm.RoleUser // silence unused imports in some toolchains
}

// cosine similarity for test
func cos(a, b []float32) float32 {
	var dot, na, nb float32
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (fsqrt(na) * fsqrt(nb))
}

func fsqrt(x float32) float32 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 6; i++ {
		z = 0.5 * (z + x/z)
	}
	return z
}
