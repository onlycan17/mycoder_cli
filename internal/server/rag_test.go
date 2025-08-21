package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mycoder/internal/llm"
	"mycoder/internal/store"
)

func TestWithRAGContextTrustRerank(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte("func A(){}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.go"), []byte("func A(){}\n"), 0o644)
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", dir, nil)
	st.AddDocument(p.ID, "a.go", "func A(){}\n")
	st.AddDocument(p.ID, "b.go", "func A(){}\n")
	// knowledge: a.go has higher trust
	_, _ = st.AddKnowledge(p.ID, "code", "a.go", "A impl", "desc", 0.9, true)

	msgs := []llm.Message{{Role: llm.RoleUser, Content: "func A"}}
	out := api.withRAGContext(msgs, p.ID, 2)
	if len(out) < 2 {
		t.Fatalf("expected system+user")
	}
	sys := out[0].Content
	ia := strings.Index(sys, "a.go")
	ib := strings.Index(sys, "b.go")
	if ia == -1 || ib == -1 || ia > ib {
		t.Fatalf("expected a.go before b.go in system context: %q", sys)
	}
}
