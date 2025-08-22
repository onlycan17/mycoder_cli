package store

import (
	"mycoder/internal/models"
	"path/filepath"
	"testing"
)

func TestUpsertSymbols(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "sym.db")
	s, err := NewSQLite(dbpath)
	if err != nil {
		t.Skip("sqlite not available:", err)
	}
	p := s.CreateProject("symproj", dir, nil)
	syms := []models.Symbol{{Name: "Foo", Kind: "type", StartLine: 1, EndLine: 1}, {Name: "Bar", Kind: "func", StartLine: 2, EndLine: 3}}
	if err := s.UpsertSymbols(p.ID, "a.go", "go", syms); err != nil {
		t.Fatalf("UpsertSymbols: %v", err)
	}
	var cnt int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM symbols WHERE project_id=? AND path=?`, p.ID, "a.go").Scan(&cnt); err != nil {
		t.Fatalf("count symbols: %v", err)
	}
	if cnt != 2 {
		t.Fatalf("expected 2 symbols, got %d", cnt)
	}
}
