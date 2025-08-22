package store

import (
	"path/filepath"
	"testing"
)

func TestDocChunkerSplitsMarkdownByHeadings(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "doc.db")
	s, err := NewSQLite(dbpath)
	if err != nil {
		t.Skip("sqlite not available:", err)
	}

	p := s.CreateProject("docs", dir, nil)
	md := "# Title\npara one\n\n# Section\npara two\n"
	d := s.UpsertDocument(p.ID, "README.md", md, "sha1", "md", "2025-08-21T00:00:00Z")
	if d.ID == "" {
		t.Fatalf("empty document id")
	}
	var cnt int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM chunks WHERE doc_id=?`, d.ID).Scan(&cnt); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if cnt < 2 {
		t.Fatalf("expected at least 2 chunks for headings, got %d", cnt)
	}
	// ensure search hits work
	res := s.Search(p.ID, "Section", 5)
	if len(res) == 0 {
		t.Fatalf("expected search hit for 'Section'")
	}
}
