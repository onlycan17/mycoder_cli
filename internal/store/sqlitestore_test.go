package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteSearchProjectFilter(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "test.db")
	s, err := NewSQLite(dbpath)
	if err != nil {
		t.Skip("sqlite not available:", err)
	}

	p1 := s.CreateProject("p1", dir, nil)
	p2 := s.CreateProject("p2", dir, nil)

	s.AddDocument(p1.ID, "a.txt", "alpha beta gamma")
	s.AddDocument(p2.ID, "b.txt", "alpha delta")

	// query within p1 only
	got := s.Search(p1.ID, "alpha", 10)
	if len(got) != 1 || got[0].Path != "a.txt" {
		t.Fatalf("expected 1 result a.txt in p1, got %+v", got)
	}

	// global query (no project) should see both
	got = s.Search("", "alpha", 10)
	if len(got) < 2 {
		t.Fatalf("expected >=2 results globally, got %d", len(got))
	}

	// wrong project filter
	got = s.Search("nonexistent", "alpha", 10)
	if len(got) != 0 {
		t.Fatalf("expected 0 results with wrong project filter, got %d", len(got))
	}

	_ = os.Remove(dbpath)
}

func TestSQLiteUpsertUpdatesContent(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "test2.db")
	s, err := NewSQLite(dbpath)
	if err != nil {
		t.Skip("sqlite not available:", err)
	}

	p := s.CreateProject("p", dir, nil)
	s.UpsertDocument(p.ID, "a.txt", "alpha", "sha1", "txt")
	got := s.Search(p.ID, "delta", 10)
	if len(got) != 0 {
		t.Fatalf("expected 0 results before update, got %d", len(got))
	}

	s.UpsertDocument(p.ID, "a.txt", "alpha delta", "sha2", "txt")
	got = s.Search(p.ID, "delta", 10)
	if len(got) == 0 {
		t.Fatalf("expected results after update")
	}
}

func TestKnowledgeOperations(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "knowledge_test.db")
	s, err := NewSQLite(dbpath)
	if err != nil {
		t.Skip("sqlite not available:", err)
	}
	defer os.Remove(dbpath)

	p := s.CreateProject("knowledge_proj", dir, nil)

	// 1. Add Knowledge
	k, err := s.AddKnowledge(p.ID, "manual", "", "Test Title", "Test text", 0.5, true)
	if err != nil {
		t.Fatalf("AddKnowledge failed: %v", err)
	}
	if k.ID == "" {
		t.Fatal("expected knowledge ID, got empty string")
	}

	// 2. List Knowledge
	items, err := s.ListKnowledge(p.ID, 0.0)
	if err != nil {
		t.Fatalf("ListKnowledge failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 knowledge item, got %d", len(items))
	}
	if items[0].Title != "Test Title" {
		t.Errorf("expected title 'Test Title', got '%s'", items[0].Title)
	}

	// 3. List with minScore filter
	items, err = s.ListKnowledge(p.ID, 0.6)
	if err != nil {
		t.Fatalf("ListKnowledge with minScore failed: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 knowledge items with minScore 0.6, got %d", len(items))
	}

	// 4. Vet Knowledge
	affected, err := s.VetKnowledge(p.ID)
	if err != nil {
		t.Fatalf("VetKnowledge failed: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 row affected by VetKnowledge, got %d", affected)
	}

	// 5. Verify trust score update
	items, err = s.ListKnowledge(p.ID, 0.5) // Original score was 0.5, should be ~0.6 now
	if err != nil {
		t.Fatalf("ListKnowledge after VetKnowledge failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 knowledge item after vet, got %d", len(items))
	}
	if items[0].TrustScore <= 0.5 {
		t.Errorf("expected trust score > 0.5 after vet, got %f", items[0].TrustScore)
	}
}
