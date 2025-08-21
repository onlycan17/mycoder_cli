package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

import (
	"encoding/json"
	"mycoder/internal/indexer"
	"mycoder/internal/llm"
	oai "mycoder/internal/llm/openai"
	"mycoder/internal/models"
	"mycoder/internal/store"
	"strconv"
)

type Store interface {
	CreateProject(name, root string, ignore []string) *models.Project
	ListProjects() []*models.Project
	GetProject(id string) (*models.Project, bool)
	// jobs
	CreateIndexJob(projectID string, mode models.IndexMode) (*models.IndexJob, error)
	SetJobStatus(id string, st models.IndexJobStatus, stats map[string]int) (*models.IndexJob, error)
	GetJob(id string) (*models.IndexJob, bool)
	// docs/search
	AddDocument(projectID, path, content string) *models.Document
	Search(projectID, query string, k int) []models.SearchResult
	// metrics
	Stats() map[string]int
	// knowledge
	AddKnowledge(projectID, sourceType, pathOrURL, title, text string, trust float64, pinned bool) (*models.Knowledge, error)
	ListKnowledge(projectID string, minScore float64) ([]*models.Knowledge, error)
	VetKnowledge(projectID string) (int, error)
	PromoteKnowledge(projectID, title, text, pathOrURL, commitSHA, filesCSV, symbolsCSV string, pin bool) (*models.Knowledge, error)
	ReverifyKnowledge(projectID string) (int, error)
	GCKnowledge(projectID string, minScore float64) (int, error)
}

type IncrementalStore interface {
	UpsertDocument(projectID, path, content, sha, lang string) *models.Document
	PruneDocuments(projectID string, present []string) error
}

type API struct {
	store Store
	llm   llm.ChatProvider
}

func NewAPI(s Store, p llm.ChatProvider) *API { return &API{store: s, llm: p} }

func (a *API) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/projects", a.handleProjects)
	mux.HandleFunc("/index/run", a.handleIndexRun)
	mux.HandleFunc("/index/jobs/", a.handleIndexJob)
	mux.HandleFunc("/search", a.handleSearch)
	mux.HandleFunc("/metrics", a.handleMetrics)
	mux.HandleFunc("/fs/read", a.handleFSRead)
	mux.HandleFunc("/fs/write", a.handleFSWrite)
	mux.HandleFunc("/fs/patch", a.handleFSPatch)
	mux.HandleFunc("/fs/delete", a.handleFSDelete)
	mux.HandleFunc("/shell/exec", a.handleShellExec)
	mux.HandleFunc("/shell/exec/stream", a.handleShellExecStream)
	mux.HandleFunc("/chat", a.handleChat)
	// knowledge curation
	mux.HandleFunc("/knowledge", a.handleKnowledge)
	mux.HandleFunc("/knowledge/vet", a.handleKnowledgeVet)
	mux.HandleFunc("/knowledge/promote", a.handleKnowledgePromote)
	mux.HandleFunc("/knowledge/reverify", a.handleKnowledgeReverify)
	mux.HandleFunc("/knowledge/gc", a.handleKnowledgeGC)
	mux.HandleFunc("/knowledge/promote/auto", a.handleKnowledgePromoteAuto)
	return mux
}

// Run starts an HTTP server with a minimal health endpoint.
func Run(addr string) error {
	var st Store
	if path := os.Getenv("MYCODER_SQLITE_PATH"); path != "" {
		if sdb, err := store.NewSQLite(path); err == nil {
			st = sdb
		} else {
			fmt.Fprintf(os.Stderr, "sqlite init failed: %v, falling back to memory\n", err)
			st = store.New()
		}
	} else {
		st = store.New()
	}
	// select LLM provider
	var prov llm.ChatProvider
	switch strings.ToLower(os.Getenv("MYCODER_LLM_PROVIDER")) {
	case "", "openai":
		prov = oai.NewFromEnv()
	default:
		prov = oai.NewFromEnv()
	}
	api := NewAPI(st, prov)
	mux := api.mux()
	// optional background curator (reverify/decay)
	if os.Getenv("MYCODER_CURATOR_DISABLE") == "" {
		interval := 10 * time.Minute
		if v := os.Getenv("MYCODER_CURATOR_INTERVAL"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				interval = d
			}
		}
		minTrust := 0.4
		if v := os.Getenv("MYCODER_KNOWLEDGE_MIN_TRUST"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				minTrust = f
			}
		}
		go func() {
			t := time.NewTicker(interval)
			defer t.Stop()
			for range t.C {
				for _, p := range st.ListProjects() {
					_, _ = st.ReverifyKnowledge(p.ID)
					_, _ = st.GCKnowledge(p.ID, minTrust)
				}
			}
		}()
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           logMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		errs <- srv.ListenAndServe()
	}()

	// graceful shutdown on SIGINT/SIGTERM
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigc:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return fmt.Errorf("shutdown by signal: %v", sig)
	case err := <-errs:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// minimal access log (stdout)
		start := time.Now()
		next.ServeHTTP(w, r)
		fmt.Fprintf(os.Stdout, "%s %s %s\n", r.Method, r.URL.Path, time.Since(start))
	})
}

// Handlers
func (a *API) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list := a.store.ListProjects()
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var req struct {
			Name     string   `json:"name"`
			RootPath string   `json:"rootPath"`
			Ignore   []string `json:"ignore"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.RootPath == "" {
			http.Error(w, "name and rootPath required", http.StatusBadRequest)
			return
		}
		p := a.store.CreateProject(req.Name, req.RootPath, req.Ignore)
		writeJSON(w, http.StatusOK, map[string]string{"projectID": p.ID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) handleIndexRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ProjectID string           `json:"projectID"`
		Mode      models.IndexMode `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ProjectID == "" {
		http.Error(w, "projectID required", http.StatusBadRequest)
		return
	}
	if req.Mode == "" {
		req.Mode = models.IndexFull
	}
	job, err := a.store.CreateIndexJob(req.ProjectID, req.Mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 비동기 처리(즉시 완료 스텁 구현)
	go func(id string) {
		_, _ = a.store.SetJobStatus(id, models.JobRunning, nil)
		// fetch project root
		if p, ok := a.store.GetProject(req.ProjectID); ok {
			docs, _ := indexer.Index(p.RootPath, indexer.Options{MaxFiles: 500, MaxFileSize: 256 * 1024})
			// incremental if supported
			if inc, ok := a.store.(IncrementalStore); ok {
				present := make([]string, 0, len(docs))
				for _, d := range docs {
					inc.UpsertDocument(p.ID, d.Path, d.Content, d.SHA, d.Lang)
					present = append(present, d.Path)
				}
				_ = inc.PruneDocuments(p.ID, present)
			} else {
				for _, d := range docs {
					a.store.AddDocument(p.ID, d.Path, d.Content)
				}
			}
			stats := map[string]int{"documents": len(docs)}
			_, _ = a.store.SetJobStatus(id, models.JobCompleted, stats)
			return
		}
		_, _ = a.store.SetJobStatus(id, models.JobFailed, map[string]int{"documents": 0})
	}(job.ID)

	writeJSON(w, http.StatusOK, map[string]string{"jobID": job.ID})
}

func (a *API) handleIndexJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// path: /index/jobs/:id
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/index/jobs/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "job id required", http.StatusBadRequest)
		return
	}
	id := parts[0]
	if job, ok := a.store.GetJob(id); ok {
		writeJSON(w, http.StatusOK, job)
		return
	}
	http.NotFound(w, r)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		http.Error(w, "q required", http.StatusBadRequest)
		return
	}
	k := 10
	pid := r.URL.Query().Get("projectID")
	results := a.store.Search(pid, q, k)
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (a *API) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, a.store.Stats())
}

// Knowledge handlers
func (a *API) handleKnowledge(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			ProjectID, SourceType, PathOrURL, Title, Text string
			TrustScore                                    float64
			Pinned                                        bool
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.SourceType == "" || req.Text == "" {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		k, err := a.store.AddKnowledge(req.ProjectID, req.SourceType, req.PathOrURL, req.Title, req.Text, req.TrustScore, req.Pinned)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, k)
	case http.MethodGet:
		pid := r.URL.Query().Get("projectID")
		if pid == "" {
			http.Error(w, "projectID required", http.StatusBadRequest)
			return
		}
		min := 0.0
		list, err := a.store.ListKnowledge(pid, min)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"knowledge": list})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) handleKnowledgeVet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct{ ProjectID string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	n, err := a.store.VetKnowledge(req.ProjectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"vettedCount": n})
}

func (a *API) handleKnowledgePromote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ProjectID, Title, Text, PathOrURL, CommitSHA, Files, Symbols string
		Pin                                                          bool
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.Text == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	k, err := a.store.PromoteKnowledge(req.ProjectID, req.Title, req.Text, req.PathOrURL, req.CommitSHA, req.Files, req.Symbols, req.Pin)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, k)
}

func (a *API) handleKnowledgeReverify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct{ ProjectID string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	n, err := a.store.ReverifyKnowledge(req.ProjectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"updated": n})
}

func (a *API) handleKnowledgeGC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ProjectID string
		Min       float64
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	n, err := a.store.GCKnowledge(req.ProjectID, req.Min)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": n})
}

// Auto-promote: summarize given files with LLM (if configured) and create Knowledge.
func (a *API) handleKnowledgePromoteAuto(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ProjectID string
		Files     []string
		Title     string
		Pin       bool
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if len(req.Files) == 0 {
		http.Error(w, "files required", http.StatusBadRequest)
		return
	}
	// read snippets from files (cap budget)
	p, ok := a.store.GetProject(req.ProjectID)
	if !ok {
		http.Error(w, "project not found", http.StatusBadRequest)
		return
	}
	var b strings.Builder
	budget := 4000
	for _, rel := range req.Files {
		_, full, ok := a.resolveProjectPath(req.ProjectID, rel)
		if !ok {
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		header := fmt.Sprintf("\n=== %s ===\n", rel)
		if budget-len(header) <= 0 {
			break
		}
		b.WriteString(header)
		budget -= len(header)
		// include up to 800 chars per file
		max := 800
		if max > len(data) {
			max = len(data)
		}
		chunk := string(data[:max])
		if budget-len(chunk) <= 0 {
			break
		}
		b.WriteString(chunk)
		budget -= len(chunk)
	}
	content := b.String()
	summary := ""
	// use LLM if available
	if a.llm != nil && content != "" {
		sys := llm.Message{Role: llm.RoleSystem, Content: "You are a senior engineer. Summarize the following code changes into a concise 'CodeCard' (purpose, approach, key decisions, trade-offs). Keep it under 800 chars."}
		usr := llm.Message{Role: llm.RoleUser, Content: content}
		st, err := a.llm.Chat(r.Context(), os.Getenv("MYCODER_CHAT_MODEL"), []llm.Message{sys, usr}, false, 0)
		if err == nil {
			defer st.Close()
			var buf strings.Builder
			for {
				d, done, e := st.Recv()
				if e != nil {
					break
				}
				buf.WriteString(d)
				if done {
					break
				}
			}
			summary = buf.String()
		}
	}
	if summary == "" { // fallback naive summary
		summary = "CodeCard: Summary of files\n" + content
	}
	title := req.Title
	if title == "" {
		title = fmt.Sprintf("Promoted knowledge: %d files", len(req.Files))
	}
	filesCSV := strings.Join(req.Files, ",")
	k, err := a.store.PromoteKnowledge(req.ProjectID, title, summary, p.RootPath, "", filesCSV, "", req.Pin)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, k)
}

// FS handlers (project-root confined)
func (a *API) handleFSRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct{ ProjectID, Path string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.Path == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	root, full, ok := a.resolveProjectPath(req.ProjectID, req.Path)
	_ = root
	if !ok {
		http.Error(w, "path outside project", http.StatusForbidden)
		return
	}
	b, err := os.ReadFile(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": req.Path, "content": string(b)})
}

func (a *API) handleFSWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct{ ProjectID, Path, Content string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.Path == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	_, full, ok := a.resolveProjectPath(req.ProjectID, req.Path)
	if !ok {
		http.Error(w, "path outside project", http.StatusForbidden)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(full, []byte(req.Content), 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) handleFSDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct{ ProjectID, Path string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.Path == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	_, full, ok := a.resolveProjectPath(req.ProjectID, req.Path)
	if !ok {
		http.Error(w, "path outside project", http.StatusForbidden)
		return
	}
	if err := os.Remove(full); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) resolveProjectPath(projectID, rel string) (string, string, bool) {
	p, ok := a.store.GetProject(projectID)
	if !ok {
		return "", "", false
	}
	root := p.RootPath
	full := filepath.Clean(filepath.Join(root, rel))
	// ensure inside root
	pr, err := filepath.Abs(root)
	if err != nil {
		return "", "", false
	}
	pf, err := filepath.Abs(full)
	if err != nil {
		return "", "", false
	}
	if !strings.HasPrefix(pf+string(os.PathSeparator), pr+string(os.PathSeparator)) && pf != pr {
		return "", "", false
	}
	return root, full, true
}

func (a *API) handleFSPatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ProjectID string `json:"projectID"`
		Path      string `json:"path"`
		Hunks     []struct {
			Start   int    `json:"start"`
			Length  int    `json:"length"`
			Replace string `json:"replace"`
		} `json:"hunks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.Path == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	_, full, ok := a.resolveProjectPath(req.ProjectID, req.Path)
	if !ok {
		http.Error(w, "path outside project", http.StatusForbidden)
		return
	}
	b, err := os.ReadFile(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	// apply hunks in order; assume Start/Length are byte offsets
	buf := b
	offset := 0
	for i, h := range req.Hunks {
		start := h.Start + offset
		end := start + h.Length
		if start < 0 || end < start || end > len(buf) {
			http.Error(w, fmt.Sprintf("invalid hunk %d", i), http.StatusBadRequest)
			return
		}
		// build new buffer
		nb := make([]byte, 0, len(buf)-h.Length+len(h.Replace))
		nb = append(nb, buf[:start]...)
		nb = append(nb, []byte(h.Replace)...)
		nb = append(nb, buf[end:]...)
		// update offset for subsequent hunks
		offset += len(h.Replace) - h.Length
		buf = nb
	}
	if err := os.WriteFile(full, buf, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) handleShellExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ProjectID, Cmd string
		Args           []string
		TimeoutSec     int
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.Cmd == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	p, ok := a.store.GetProject(req.ProjectID)
	if !ok {
		http.Error(w, "project not found", http.StatusBadRequest)
		return
	}
	to := time.Duration(30) * time.Second
	if req.TimeoutSec > 0 {
		to = time.Duration(req.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), to)
	defer cancel()
	// Build a zsh -lc commandline so users can use shell semantics.
	cmdline := buildCmdline(req.Cmd, req.Args)
	cmd := exec.CommandContext(ctx, "/bin/zsh", "-lc", cmdline)
	cmd.Dir = p.RootPath
	out, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			exit = -1
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"exitCode": exit, "output": string(out)})
}

func (a *API) handleShellExecStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ProjectID, Cmd string
		Args           []string
		TimeoutSec     int
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.Cmd == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	p, ok := a.store.GetProject(req.ProjectID)
	if !ok {
		http.Error(w, "project not found", http.StatusBadRequest)
		return
	}
	to := time.Duration(60) * time.Second
	if req.TimeoutSec > 0 {
		to = time.Duration(req.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), to)
	defer cancel()
	cmdline := buildCmdline(req.Cmd, req.Args)
	cmd := exec.CommandContext(ctx, "/bin/zsh", "-lc", cmdline)
	cmd.Dir = p.RootPath
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fl, _ := w.(http.Flusher)
	send := func(event, data string) {
		fmt.Fprintf(w, "event: %s\n", event)
		fmt.Fprintf(w, "data: %s\n\n", data)
		if fl != nil {
			fl.Flush()
		}
	}
	go streamReader(stdout, func(line string) { send("stdout", line) })
	go streamReader(stderr, func(line string) { send("stderr", line) })
	err := cmd.Wait()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	send("exit", fmt.Sprintf("%d", code))
}

func streamReader(r io.Reader, fn func(string)) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			fn(strings.TrimRight(string(buf[:n]), "\n"))
		}
		if err != nil {
			return
		}
	}
}

// buildCmdline concatenates command and args with basic shell-safe quoting for zsh -lc.
func buildCmdline(cmd string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, shellQuote(cmd))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	// simple single-quote escaping: ' -> '\''
	if strings.IndexByte(s, '\'') == -1 && strings.IndexByte(s, ' ') == -1 && strings.IndexAny(s, "\t\n$") == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func shellQuoteOrEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return shellQuote(s)
}

// POST /chat: {messages:[{role,content}], model?, stream?, temperature?}
func (a *API) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.llm == nil {
		http.Error(w, "llm provider not configured", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Messages    []llm.Message `json:"messages"`
		Model       string        `json:"model"`
		Stream      bool          `json:"stream"`
		Temperature float32       `json:"temperature"`
		ProjectID   string        `json:"projectID"`
		Retrieval   struct {
			K int `json:"k"`
		} `json:"retrieval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	msgs := req.Messages
	if req.ProjectID != "" {
		k := req.Retrieval.K
		if k <= 0 {
			k = 5
		}
		msgs = a.withRAGContext(msgs, req.ProjectID, k)
	}
	st, err := a.llm.Chat(r.Context(), req.Model, msgs, req.Stream, req.Temperature)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer st.Close()
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fl, _ := w.(http.Flusher)
		for {
			delta, done, err := st.Recv()
			if err != nil {
				fmt.Fprintf(w, "event: error\n\n")
				if fl != nil {
					fl.Flush()
				}
				return
			}
			if delta != "" {
				fmt.Fprintf(w, "event: token\n")
				fmt.Fprintf(w, "data: %s\n\n", jsonEscape(delta))
				if fl != nil {
					fl.Flush()
				}
			}
			if done {
				fmt.Fprintf(w, "event: done\n\n")
				if fl != nil {
					fl.Flush()
				}
				return
			}
		}
	}
	var buf strings.Builder
	for {
		delta, done, err := st.Recv()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		buf.WriteString(delta)
		if done {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": buf.String()})
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return string(b)
}

// withRAGContext builds a simple context message using lexical search results for the latest user query.
func (a *API) withRAGContext(messages []llm.Message, projectID string, k int) []llm.Message {
	var q string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			q = messages[i].Content
			break
		}
	}
	if strings.TrimSpace(q) == "" {
		return messages
	}
	raw := a.store.Search(projectID, q, k*2)
	if len(raw) == 0 {
		return messages
	}
	// trustScore-aware rerank: adjust search score with knowledge trust per path
	trust := make(map[string]float64)
	if kn, err := a.store.ListKnowledge(projectID, 0.0); err == nil {
		for _, kv := range kn {
			if kv.PathOrURL != "" && kv.TrustScore > trust[kv.PathOrURL] {
				trust[kv.PathOrURL] = kv.TrustScore
			}
		}
	}
	type scored struct {
		s   models.SearchResult
		adj float64
	}
	cand := make([]scored, 0, len(raw))
	const alpha = 1.0
	for _, h := range raw {
		adj := h.Score + alpha*trust[h.Path]
		cand = append(cand, scored{s: h, adj: adj})
	}
	sort.SliceStable(cand, func(i, j int) bool { return cand[i].adj > cand[j].adj })
	seen := make(map[string]bool)
	hits := make([]models.SearchResult, 0, k)
	for _, c := range cand {
		if seen[c.s.Path] {
			continue
		}
		seen[c.s.Path] = true
		hits = append(hits, c.s)
		if len(hits) >= k {
			break
		}
	}
	// prepend curated knowledge heads (titles/links) if exists
	if kn, err := a.store.ListKnowledge(projectID, 0.5); err == nil && len(kn) > 0 {
		var kb strings.Builder
		kb.WriteString("Curated Knowledge:\n")
		max := 3
		if len(kn) < max {
			max = len(kn)
		}
		for i := 0; i < max; i++ {
			kb.WriteString("- ")
			if kn[i].Title != "" {
				kb.WriteString(kn[i].Title)
			} else {
				kb.WriteString(kn[i].PathOrURL)
			}
			kb.WriteString("\n")
		}
		sys := llm.Message{Role: llm.RoleSystem, Content: kb.String()}
		messages = append([]llm.Message{sys}, messages...)
	}
	var b strings.Builder
	b.WriteString("You are a coding assistant. Use the following repo context and cite files with line ranges. If not enough evidence, say you are unsure.\n\n")
	b.WriteString("Context:\n")
	budget := 3000
	var root string
	if p, ok := a.store.GetProject(projectID); ok {
		root = p.RootPath
	}
	for _, h := range hits {
		loc := h.Path
		if h.StartLine > 0 {
			if h.EndLine > 0 && h.EndLine != h.StartLine {
				loc = fmt.Sprintf("%s:%d-%d", h.Path, h.StartLine, h.EndLine)
			} else {
				loc = fmt.Sprintf("%s:%d", h.Path, h.StartLine)
			}
		}
		b.WriteString("- ")
		b.WriteString(loc)
		if strings.TrimSpace(h.Preview) != "" {
			b.WriteString(" — ")
			b.WriteString(h.Preview)
		}
		b.WriteString("\n")
		if root != "" {
			code := readSnippet(root, h.Path, h.StartLine, h.EndLine, 24)
			if code != "" {
				block := fmt.Sprintf("```%s\n%s\n```\n", fenceLangFor(h.Path), code)
				if budget-len(block) > 0 {
					b.WriteString(block)
					budget -= len(block)
				}
			}
		}
		if budget <= 0 {
			break
		}
	}
	sys := llm.Message{Role: llm.RoleSystem, Content: b.String()}
	out := make([]llm.Message, 0, len(messages)+1)
	out = append(out, sys)
	out = append(out, messages...)
	return out
}

// readSnippet reads lines [start:end] with margins; clamps to file bounds.
func readSnippet(root, rel string, start, end, maxLines int) string {
	full := filepath.Clean(filepath.Join(root, rel))
	data, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if start <= 0 {
		start = 1
	}
	if end <= 0 || end > len(lines) {
		end = start
	}
	margin := 2
	s := start - margin
	if s < 1 {
		s = 1
	}
	e := end + margin
	if e > len(lines) {
		e = len(lines)
	}
	if maxLines > 0 && e-s+1 > maxLines {
		e = s + maxLines - 1
	}
	return strings.Join(lines[s-1:e], "\n")
}

func fenceLangFor(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "ts"
	case ".js", ".jsx":
		return "js"
	case ".py":
		return "py"
	case ".md":
		return "md"
	case ".json":
		return "json"
	case ".yml", ".yaml":
		return "yaml"
	default:
		return ""
	}
}
