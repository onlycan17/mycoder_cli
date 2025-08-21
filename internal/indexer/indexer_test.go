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
