package store

import (
	"path/filepath"
	"testing"
)

func TestProjectAndDocumentCRUD(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "crud.db")
	s, err := NewSQLite(dbpath)
	if err != nil {
		t.Skip("sqlite not available:", err)
	}

	// create project and verify list/get
	p := s.CreateProject("proj-crud", dir, nil)
	if p.ID == "" {
		t.Fatalf("empty project id")
	}
	list := s.ListProjects()
	if len(list) == 0 {
		t.Fatalf("expected at least 1 project in list")
	}
	if _, ok := s.GetProject(p.ID); !ok {
		t.Fatalf("project not found by id")
	}

	// document add/get/search
	s.AddDocument(p.ID, "d.txt", "hello world")
	if doc, ok := s.GetDocument(p.ID, "d.txt"); !ok || doc.Path != "d.txt" {
		t.Fatalf("expected to get document d.txt, got ok=%v doc=%v", ok, doc)
	}
	if res := s.Search(p.ID, "hello", 10); len(res) == 0 {
		t.Fatalf("expected search hit for 'hello'")
	}

	// delete document and ensure gone
	if err := s.DeleteDocument(p.ID, "d.txt"); err != nil {
		t.Fatalf("DeleteDocument error: %v", err)
	}
	if _, ok := s.GetDocument(p.ID, "d.txt"); ok {
		t.Fatalf("expected document to be deleted")
	}

	// delete project cascades
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatalf("DeleteProject error: %v", err)
	}
	if _, ok := s.GetProject(p.ID); ok {
		t.Fatalf("expected project to be deleted")
	}
}
