package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexBasic(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "code.go"), []byte("package x\nfunc A(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// binary-like file should be skipped
	_ = os.WriteFile(filepath.Join(dir, "bin.dat"), append([]byte{0, 1, 2}, []byte("x")...), 0o644)

	docs, err := Index(dir, Options{MaxFiles: 10, MaxFileSize: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
}

func TestIndexIncludeExclude(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.md"), []byte("# doc\n"), 0o644)
	// include only *.md
	docs, err := Index(dir, Options{MaxFiles: 10, MaxFileSize: 1024, Include: []string{"*.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Path != "b.md" {
		t.Fatalf("include filter failed: %+v", docs)
	}
	// exclude *.md
	docs, err = Index(dir, Options{MaxFiles: 10, MaxFileSize: 1024, Exclude: []string{"*.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Path != "a.go" {
		t.Fatalf("exclude filter failed: %+v", docs)
	}
}
