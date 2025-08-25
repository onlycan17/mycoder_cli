package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mycoder/internal/llm"
	"mycoder/internal/store"
)

// Verify that RAG budget/avg line bytes/min/max lines envs affect snippet size.
func TestRAGSnippetLineBudgetEnv(t *testing.T) {
	dir := t.TempDir()
	// prepare a file with many lines
	lines := make([]string, 50)
	for i := 0; i < len(lines); i++ {
		lines[i] = "line"
	}
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte(strings.Join(lines, "\n")), 0o644)

	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", dir, nil)
	st.AddDocument(p.ID, "a.go", strings.Join(lines, "\n"))

	// very small byte budget and average line bytes to force small max lines
	os.Setenv("MYCODER_RAG_BUDGET_BYTES", "120")
	os.Setenv("MYCODER_RAG_AVG_LINE_BYTES", "20") // est ~6 lines cap by max/min
	os.Setenv("MYCODER_RAG_MIN_LINES_PER_SNIPPET", "3")
	os.Setenv("MYCODER_RAG_MAX_LINES_CAP", "5")
	os.Setenv("MYCODER_RAG_SNIPPET_MARGIN_LINES", "0")
	t.Cleanup(func() {
		os.Unsetenv("MYCODER_RAG_BUDGET_BYTES")
		os.Unsetenv("MYCODER_RAG_AVG_LINE_BYTES")
		os.Unsetenv("MYCODER_RAG_MIN_LINES_PER_SNIPPET")
		os.Unsetenv("MYCODER_RAG_MAX_LINES_CAP")
		os.Unsetenv("MYCODER_RAG_SNIPPET_MARGIN_LINES")
	})

	msgs := []llm.Message{{Role: llm.RoleUser, Content: "line"}}
	out := api.withRAGContext(msgs, p.ID, 1)
	if len(out) == 0 {
		t.Fatalf("no messages returned")
	}
	sys := out[0].Content
	// the code block should contain at most 5 lines
	// count lines inside code fence by extracting lines between ``` markers
	idx := strings.Index(sys, "```")
	if idx < 0 {
		t.Fatalf("no code fence found in system context: %q", sys)
	}
	rest := sys[idx+3:]
	// language line
	nl := strings.Index(rest, "\n")
	if nl < 0 {
		t.Fatalf("no newline after fence lang")
	}
	content := rest[nl+1:]
	end := strings.Index(content, "```\n")
	if end < 0 {
		end = len(content)
	}
	code := content[:end]
	gotLines := len(strings.Split(strings.TrimSuffix(code, "\n"), "\n"))
	if gotLines > 5 {
		t.Fatalf("expected at most 5 lines, got %d", gotLines)
	}
}
