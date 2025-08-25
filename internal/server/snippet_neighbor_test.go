package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mycoder/internal/llm"
	"mycoder/internal/store"
)

func TestNeighborExpansionToFunctionBoundary(t *testing.T) {
	dir := t.TempDir()
	src := "package main\n\nfunc Hello() {\n    // marker\n    println(\"hi\")\n}\n\nfunc Tail() {}\n"
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644)

	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", dir, nil)
	st.AddDocument(p.ID, "a.go", src)

	// enable neighbor expansion and generous limits
	os.Setenv("MYCODER_RAG_NEIGHBOR_ENABLE", "1")
	os.Setenv("MYCODER_RAG_NEIGHBOR_MAX_LINES", "50")
	os.Setenv("MYCODER_RAG_BUDGET_BYTES", "2000")
	os.Setenv("MYCODER_RAG_AVG_LINE_BYTES", "40")
	t.Cleanup(func() {
		os.Unsetenv("MYCODER_RAG_NEIGHBOR_ENABLE")
		os.Unsetenv("MYCODER_RAG_NEIGHBOR_MAX_LINES")
		os.Unsetenv("MYCODER_RAG_BUDGET_BYTES")
		os.Unsetenv("MYCODER_RAG_AVG_LINE_BYTES")
	})

	// query around the marker line so StartLine/EndLine likely land inside function
	msgs := []llm.Message{{Role: llm.RoleUser, Content: "marker"}}
	out := api.withRAGContext(msgs, p.ID, 1)
	if len(out) == 0 {
		t.Fatalf("no messages returned")
	}
	sys := out[0].Content
	// the code block should include the function signature and closing brace
	if !strings.Contains(sys, "func Hello() {") {
		t.Fatalf("expected expanded snippet to include function start, got: %q", sys)
	}
	if !strings.Contains(sys, "}\n\nfunc Tail()") && !strings.Contains(sys, "}\n`") && !strings.Contains(sys, "}\n\n`") {
		if !strings.Contains(sys, "}") {
			t.Fatalf("expected closing brace in snippet, got: %q", sys)
		}
	}
}
