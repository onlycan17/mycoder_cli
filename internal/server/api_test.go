package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mycoder/internal/llm"
	"mycoder/internal/store"
)

// mockChatProvider is a mock implementation of llm.ChatProvider for testing.
type mockChatProvider struct {
	chatFn func(ctx context.Context, model string, messages []llm.Message, stream bool, temperature float32) (llm.ChatStream, error)
}

func (m *mockChatProvider) Chat(ctx context.Context, model string, messages []llm.Message, stream bool, temperature float32) (llm.ChatStream, error) {
	if m.chatFn != nil {
		return m.chatFn(ctx, model, messages, stream, temperature)
	}
	return &mockChatStream{}, nil
}

// mockChatStream is a mock implementation of llm.ChatStream for testing.
type mockChatStream struct {
	RecvFn func() (string, bool, error)
}

func (m *mockChatStream) Recv() (string, bool, error) {
	if m.RecvFn != nil {
		return m.RecvFn()
	}
	return "test", true, nil
}

func (m *mockChatStream) Close() error {
	return nil
}

func TestProjectsAndIndex(t *testing.T) {
	api := NewAPI(store.New(), &mockChatProvider{})
	mux := api.mux()

	// create project
	body := map[string]any{"name": "demo", "rootPath": "/tmp/demo"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("create project code=%d", rr.Code)
	}
	var res map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	pid := res["projectID"]
	if pid == "" {
		t.Fatal("missing projectID")
	}

	// list projects
	req = httptest.NewRequest(http.MethodGet, "/projects", nil)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list projects code=%d", rr.Code)
	}

	// run index
	b, _ = json.Marshal(map[string]any{"projectID": pid, "mode": "full"})
	req = httptest.NewRequest(http.MethodPost, "/index/run", bytes.NewReader(b))
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("index run code=%d", rr.Code)
	}
	res = map[string]string{}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	jobID := res["jobID"]
	if jobID == "" {
		t.Fatal("missing jobID")
	}

	// get job
	req = httptest.NewRequest(http.MethodGet, "/index/jobs/"+jobID, nil)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("job get code=%d", rr.Code)
	}
}

func TestSearch(t *testing.T) {
	api := NewAPI(store.New(), &mockChatProvider{})
	// seed: project + document
	p := api.store.CreateProject("demo", "/tmp/demo", nil)
	api.store.AddDocument(p.ID, "README.md", "Hello RAG\nThis project tests search API.")

	mux := api.mux()
	req := httptest.NewRequest(http.MethodGet, "/search?q=project", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("search code=%d", rr.Code)
	}
	var res struct {
		Results []map[string]any `json:"results"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if len(res.Results) == 0 {
		t.Fatalf("expected results")
	}
}

func TestKnowledgeAPI(t *testing.T) {
	api := NewAPI(store.New(), &mockChatProvider{})
	mux := api.mux()

	// Setup: create a project first
	p := api.store.CreateProject("knowledge-api-test", "/tmp/know", nil)

	// 1. POST /knowledge to add a new item
	postBody := map[string]any{
		"projectID":  p.ID,
		"sourceType": "manual",
		"title":      "API Test Knowledge",
		"text":       "This is a test.",
		"trustScore": 0.8,
		"pinned":     true,
	}
	b, _ := json.Marshal(postBody)
	req := httptest.NewRequest(http.MethodPost, "/knowledge", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST /knowledge code=%d, body=%s", rr.Code, rr.Body.String())
	}
	var postRes map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &postRes)
	if postRes["id"] == nil {
		t.Fatal("expected knowledge item with ID in response")
	}

	// 2. GET /knowledge to list items
	req = httptest.NewRequest(http.MethodGet, "/knowledge?projectID="+p.ID, nil)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /knowledge code=%d", rr.Code)
	}
	var getRes struct {
		Knowledge []map[string]any `json:"knowledge"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &getRes)
	if len(getRes.Knowledge) != 1 {
		t.Fatalf("expected 1 knowledge item, got %d", len(getRes.Knowledge))
	}
	if getRes.Knowledge[0]["title"] != "API Test Knowledge" {
		t.Errorf("unexpected title in GET response: %v", getRes.Knowledge[0]["title"])
	}

	// 3. POST /knowledge/vet to vet items
	vetBody := map[string]any{"projectID": p.ID}
	b, _ = json.Marshal(vetBody)
	req = httptest.NewRequest(http.MethodPost, "/knowledge/vet", bytes.NewReader(b))
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST /knowledge/vet code=%d", rr.Code)
	}
	var vetRes map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &vetRes)
	if vetRes["vettedCount"] != 1.0 { // JSON numbers are float64
		t.Errorf("expected vettedCount to be 1, got %v", vetRes["vettedCount"])
	}
}

// verify that withRAGContext injects a system message with citations and code block within budget.
func TestWithRAGContextInjectsContext(t *testing.T) {
	dir := t.TempDir()
	// prepare filesystem file
	content := "package main\n// hello\nfunc A(){}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", dir, nil)
	st.AddDocument(p.ID, "a.go", content)

	msgs := []llm.Message{{Role: llm.RoleUser, Content: "func A"}}
	out := api.withRAGContext(msgs, p.ID, 3)
	if len(out) < 2 {
		t.Fatalf("expected system+user messages, got %d", len(out))
	}
	if out[0].Role != llm.RoleSystem {
		t.Fatalf("first must be system, got %s", out[0].Role)
	}
	sys := out[0].Content
	if sys == "" || sys == out[1].Content {
		t.Fatalf("system content not injected or is same as user message")
	}
	if !strings.Contains(sys, "a.go") {
		t.Errorf("system message should contain citation: %s", sys)
	}
	if len(sys) > 4000 {
		t.Errorf("budget exceeded: %d", len(sys))
	}
}

func TestChatRAG(t *testing.T) {
	dir := t.TempDir()
	content := "package main\n// relevant code\nfunc MyFunc(){}\n"
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	st := store.New()
	p := st.CreateProject("p-rag", dir, nil)
	st.AddDocument(p.ID, "code.go", content)

	provider := &mockChatProvider{
		chatFn: func(ctx context.Context, model string, messages []llm.Message, stream bool, temperature float32) (llm.ChatStream, error) {
			if len(messages) < 2 || messages[0].Role != llm.RoleSystem {
				t.Error("expected system message with RAG context")
			}
			return &mockChatStream{}, nil
		},
	}
	api := NewAPI(st, provider)
	mux := api.mux()

	body := map[string]any{
		"projectID": p.ID,
		"k":         2,
		"messages":  []map[string]any{{"role": "user", "content": "MyFunc"}},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST /chat code=%d, body=%s", rr.Code, rr.Body.String())
	}
}

func TestFSPatchInvalidHunk(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", dir, nil)
	mux := api.mux()

	body := []byte(`{"projectID":"` + p.ID + `","path":"a.txt","hunks":[{"start":1000,"length":5,"replace":"x"}]}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/fs/patch", bytes.NewReader(body))
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("hello world")
	if got != "'hello world'" {
		t.Fatalf("quote: %q", got)
	}
	got2 := shellQuote("O'Reilly")
	expected := `'O'\''Reilly'`
	if got2 != expected {
		t.Fatalf("quote2: got %q, want %q", got2, expected)
	}
}

func TestBuildCmdline(t *testing.T) {
	cmd := buildCmdline("echo", []string{"hello world", "O'Reilly"})
	// ensure it contains quotes and no unescaped single quote sequences
	if cmd == "echo hello world O'Reilly" {
		t.Fatalf("expected quoting, got: %q", cmd)
	}
}
