package store

import (
	"path/filepath"
	"testing"
)

func TestVetKnowledgeScoring(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "vet.db")
	s, err := NewSQLite(dbpath)
	if err != nil {
		t.Skip("sqlite not available:", err)
	}

	p := s.CreateProject("p", dir, nil)
	// short text
	k1, err := s.AddKnowledge(p.ID, "doc", "", "t1", "short", 0.0, false)
	if err != nil {
		t.Fatal(err)
	}
	// long text (>=200 chars)
	long := make([]byte, 210)
	for i := range long {
		long[i] = 'a'
	}
	k2, err := s.AddKnowledge(p.ID, "doc", "", "t2", string(long), 0.0, true)
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.VetKnowledge(p.ID)
	if err != nil || n < 2 {
		t.Fatalf("vet err=%v n=%d", err, n)
	}

	list, err := s.ListKnowledge(p.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var v1, v2 float64
	for _, k := range list {
		if k.ID == k1.ID {
			v1 = k.TrustScore
		}
		if k.ID == k2.ID {
			v2 = k.TrustScore
		}
	}
	if v2 <= v1 {
		t.Fatalf("expected long+pinned to score higher: short=%f long=%f", v1, v2)
	}
}
