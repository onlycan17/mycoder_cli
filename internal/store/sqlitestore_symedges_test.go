package store

import (
	"mycoder/internal/models"
	"path/filepath"
	"testing"
)

func TestSymbolEdgesCRUD(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "edges.db")
	s, err := NewSQLite(dbpath)
	if err != nil {
		t.Skip("sqlite not available:", err)
	}
	p := s.CreateProject("edgesproj", dir, nil)

	edges := []models.SymbolEdge{{SrcName: "A", DstName: "B", Kind: "ref"}, {SrcName: "B", DstName: "C", Kind: "call"}}
	if err := s.UpsertSymbolEdges(p.ID, "a.go", edges); err != nil {
		t.Fatalf("UpsertSymbolEdges: %v", err)
	}
	list, err := s.ListSymbolEdges(p.ID, "a.go")
	if err != nil {
		t.Fatalf("ListSymbolEdges: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(list))
	}

	// replace edges
	edges2 := []models.SymbolEdge{{SrcName: "A", DstName: "C", Kind: "ref"}}
	if err := s.UpsertSymbolEdges(p.ID, "a.go", edges2); err != nil {
		t.Fatalf("UpsertSymbolEdges2: %v", err)
	}
	list, _ = s.ListSymbolEdges(p.ID, "a.go")
	if len(list) != 1 || list[0].DstName != "C" {
		t.Fatalf("unexpected edges: %+v", list)
	}
}
