package vectorstore

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"
)

// SQLiteVS implements VectorStore using the existing SQLite embeddings table.
type SQLiteVS struct {
	db *sql.DB
}

// NewSQLite returns a VectorStore backed by the given *sql.DB.
func NewSQLite(db *sql.DB) VectorStore { return SQLiteVS{db: db} }

func (s SQLiteVS) Upsert(ctx context.Context, items []UpsertItem) error {
	if len(items) == 0 || s.db == nil {
		return nil
	}
	now := time.Now().Format(time.RFC3339)
	for _, it := range items {
		// deterministic id per (project, doc, chunk, model)
		id := embedID(it.ProjectID, it.DocID, it.ChunkID, it.Model)
		vecJSON, err := json.Marshal(it.Vector)
		if err != nil {
			return err
		}
		// delete-then-insert for idempotency
		_, _ = s.db.ExecContext(ctx, `DELETE FROM embeddings WHERE id=?`, id)
		_, err = s.db.ExecContext(ctx, `INSERT INTO embeddings(id,project_id,doc_id,chunk_id,provider,model,dim,vector,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			id, it.ProjectID, it.DocID, it.ChunkID, it.Provider, it.Model, it.Dim, string(vecJSON), now,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s SQLiteVS) Search(ctx context.Context, projectID string, query []float32, k int) ([]Result, error) {
	if s.db == nil || len(query) == 0 || k <= 0 {
		return nil, nil
	}
	// Filter by dimension to avoid mixing models with different dims.
	rows, err := s.db.QueryContext(ctx, `SELECT doc_id, chunk_id, vector FROM embeddings WHERE project_id=? AND dim=?`, projectID, len(query))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]Result, 0, k*2)
	for rows.Next() {
		var docID, chunkID, vecStr string
		if err := rows.Scan(&docID, &chunkID, &vecStr); err != nil {
			return nil, err
		}
		var vec []float32
		if err := json.Unmarshal([]byte(vecStr), &vec); err != nil || len(vec) != len(query) {
			continue
		}
		score := cosine(query, vec)
		results = append(results, Result{DocID: docID, ChunkID: chunkID, Score: float64(score)})
	}
	// select top-k by score (descending)
	if len(results) == 0 {
		return nil, nil
	}
	// simple partial sort
	quickSelectTopK(results, k)
	if len(results) > k {
		results = results[:k]
	}
	return results, nil
}

func (s SQLiteVS) DeleteByDoc(ctx context.Context, projectID, docID string) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM embeddings WHERE project_id=? AND doc_id=?`, projectID, docID)
	return err
}

func embedID(projectID, docID, chunkID, model string) string {
	h := sha1.New()
	_, _ = h.Write([]byte(projectID))
	_, _ = h.Write([]byte{"|"[0]})
	_, _ = h.Write([]byte(docID))
	_, _ = h.Write([]byte{"|"[0]})
	_, _ = h.Write([]byte(chunkID))
	_, _ = h.Write([]byte{"|"[0]})
	_, _ = h.Write([]byte(model))
	return "emb-" + hex.EncodeToString(h.Sum(nil))
}

func cosine(a, b []float32) float32 {
	var dot, na, nb float32
	for i := 0; i < len(a); i++ {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	// normalize by magnitudes
	return dot / (sqrt(na) * sqrt(nb))
}

func sqrt(x float32) float32 {
	// simple Newton-Raphson for sqrt, sufficient for scoring
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 6; i++ {
		z = 0.5 * (z + x/z)
	}
	return z
}

// quickSelectTopK partially sorts the slice so the first k are the highest scores.
func quickSelectTopK(a []Result, k int) {
	if k <= 0 || len(a) <= k {
		// full sort when small
		for i := 0; i < len(a); i++ {
			for j := i + 1; j < len(a); j++ {
				if a[j].Score > a[i].Score {
					a[i], a[j] = a[j], a[i]
				}
			}
		}
		return
	}
	// naive nth-element then partial sort of top-k
	nthElement(a, k)
	// sort top k
	for i := 0; i < k; i++ {
		for j := i + 1; j < k && j < len(a); j++ {
			if a[j].Score > a[i].Score {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

func nthElement(a []Result, k int) {
	// quickselect by score descending
	var qs func(l, r, k int)
	qs = func(l, r, k int) {
		if l >= r {
			return
		}
		i, j := l, r
		pivot := a[(l+r)/2].Score
		for i <= j {
			for a[i].Score > pivot {
				i++
			}
			for a[j].Score < pivot {
				j--
			}
			if i <= j {
				a[i], a[j] = a[j], a[i]
				i++
				j--
			}
		}
		if k <= j {
			qs(l, j, k)
		} else if k >= i {
			qs(i, r, k)
		}
	}
	if k >= len(a) {
		k = len(a) - 1
	}
	if k >= 0 {
		qs(0, len(a)-1, k)
	}
}
