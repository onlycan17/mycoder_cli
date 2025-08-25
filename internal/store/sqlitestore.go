package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"mycoder/internal/models"
	sqlm "mycoder/internal/storage/sqlite"
)

type SQLiteStore struct {
	db  *sql.DB
	mu  sync.Mutex
	seq int64
	// jobs kept in memory for now
	jobs map[string]*models.IndexJob
}

func NewSQLite(path string) (*SQLiteStore, error) {
	if path == "" {
		return nil, errors.New("sqlite path required")
	}
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// migration manager with versioning
	if err := (sqlm.Manager{}).UpToLatest(context.Background(), db); err != nil {
		return nil, err
	}
	// optional seed data
	_ = (sqlm.Manager{}).Seed(context.Background(), db)
	return &SQLiteStore{db: db, jobs: make(map[string]*models.IndexJob)}, nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}

// DB exposes underlying *sql.DB for internal helpers (e.g., metadata tags update).
// Not part of the generic Store interface; use sparingly.
func (s *SQLiteStore) DB() *sql.DB { return s.db }

// WithTx provides a simple transaction wrapper that commits on nil error
// and rolls back on error. The callback must not hold the tx beyond return.
func (s *SQLiteStore) WithTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) nextID(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return fmt.Sprintf("%s-%d", prefix, s.seq)
}

// Projects
func (s *SQLiteStore) CreateProject(name, root string, ignore []string) *models.Project {
	id := s.nextID("proj")
	_, _ = s.db.Exec(`INSERT INTO projects(id,name,root_path,created_at) VALUES(?,?,?,?)`, id, name, root, time.Now().Format(time.RFC3339))
	return &models.Project{ID: id, Name: name, RootPath: root, Ignore: ignore, Created: time.Now()}
}

// Runs / Execution Logs
func (s *SQLiteStore) CreateRun(projectID, typ, status string) (*models.Run, error) {
	if status == "" {
		status = "running"
	}
	id := s.nextID("run")
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(`INSERT INTO runs(id,project_id,type,status,started_at) VALUES(?,?,?,?,?)`, id, projectID, typ, status, now)
	if err != nil {
		return nil, err
	}
	return &models.Run{ID: id, ProjectID: projectID, Type: typ, Status: status, StartedAt: time.Now()}, nil
}

func (s *SQLiteStore) FinishRun(id, status, metrics, logsRef string) error {
	now := time.Now().Format(time.RFC3339)
	if status == "" {
		status = "completed"
	}
	_, err := s.db.Exec(`UPDATE runs SET status=?, finished_at=?, metrics=?, logs_ref=? WHERE id=?`, status, now, metrics, logsRef, id)
	return err
}

func (s *SQLiteStore) AddExecutionLog(runID, kind, payloadRef string, exitCode int) (*models.ExecutionLog, error) {
	id := s.nextID("xlog")
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(`INSERT INTO execution_logs(id,run_id,kind,payload_ref,started_at,finished_at,exit_code) VALUES(?,?,?,?,?,?,?)`, id, runID, kind, payloadRef, now, now, exitCode)
	if err != nil {
		return nil, err
	}
	t := time.Now()
	return &models.ExecutionLog{ID: id, RunID: runID, Kind: kind, PayloadRef: payloadRef, StartedAt: t, FinishedAt: &t, ExitCode: exitCode}, nil
}

func (s *SQLiteStore) ListExecutionLogs(runID string) ([]*models.ExecutionLog, error) {
	rows, err := s.db.Query(`SELECT id, kind, payload_ref, started_at, finished_at, exit_code FROM execution_logs WHERE run_id=? ORDER BY started_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ExecutionLog
	for rows.Next() {
		var id, kind, payload, started, finished sql.NullString
		var exit int
		if err := rows.Scan(&id, &kind, &payload, &started, &finished, &exit); err == nil {
			var st, ft time.Time
			if started.Valid {
				st, _ = time.Parse(time.RFC3339, started.String)
			}
			if finished.Valid {
				t, _ := time.Parse(time.RFC3339, finished.String)
				ft = t
			}
			var ftPtr *time.Time
			if !ft.IsZero() {
				ftPtr = &ft
			}
			out = append(out, &models.ExecutionLog{
				ID: id.String, RunID: runID, Kind: kind.String, PayloadRef: payload.String,
				StartedAt: st, FinishedAt: ftPtr, ExitCode: exit,
			})
		}
	}
	return out, nil
}

func (s *SQLiteStore) ListProjects() []*models.Project {
	rows, err := s.db.Query(`SELECT id,name,root_path,created_at FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*models.Project
	for rows.Next() {
		var p models.Project
		var created string
		if err := rows.Scan(&p.ID, &p.Name, &p.RootPath, &created); err == nil {
			if t, _ := time.Parse(time.RFC3339, created); !t.IsZero() {
				p.Created = t
			}
			out = append(out, &p)
		}
	}
	return out
}

func (s *SQLiteStore) GetProject(id string) (*models.Project, bool) {
	row := s.db.QueryRow(`SELECT id,name,root_path,created_at FROM projects WHERE id=?`, id)
	var p models.Project
	var created string
	if err := row.Scan(&p.ID, &p.Name, &p.RootPath, &created); err != nil {
		return nil, false
	}
	if t, _ := time.Parse(time.RFC3339, created); !t.IsZero() {
		p.Created = t
	}
	return &p, true
}

// UpdateProjectName renames a project.
func (s *SQLiteStore) UpdateProjectName(id, name string) error {
	_, err := s.db.Exec(`UPDATE projects SET name=? WHERE id=?`, name, id)
	return err
}

// DeleteProject removes a project and its documents/chunks/termindex.
func (s *SQLiteStore) DeleteProject(id string) error {
	return s.WithTx(func(tx *sql.Tx) error {
		// collect doc ids to cascade delete chunks and terms
		rows, err := tx.Query(`SELECT id FROM documents WHERE project_id=?`, id)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var did string
			if err := rows.Scan(&did); err == nil {
				ids = append(ids, did)
			}
		}
		rows.Close()
		for _, did := range ids {
			if _, err := tx.Exec(`DELETE FROM termindex WHERE doc_id=?`, did); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM chunks WHERE doc_id=?`, did); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DELETE FROM documents WHERE project_id=?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM projects WHERE id=?`, id); err != nil {
			return err
		}
		return nil
	})
}

// Jobs (in-memory)
func (s *SQLiteStore) CreateIndexJob(projectID string, mode models.IndexMode) (*models.IndexJob, error) {
	if _, ok := s.GetProject(projectID); !ok {
		return nil, errors.New("project not found")
	}
	id := s.nextID("job")
	j := &models.IndexJob{ID: id, ProjectID: projectID, Mode: mode, Status: models.JobPending, StartedAt: time.Now()}
	s.mu.Lock()
	s.jobs[id] = j
	s.mu.Unlock()
	return j, nil
}

func (s *SQLiteStore) SetJobStatus(id string, st models.IndexJobStatus, stats map[string]int) (*models.IndexJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, errors.New("job not found")
	}
	j.Status = st
	if st == models.JobCompleted || st == models.JobFailed {
		now := time.Now()
		j.EndedAt = &now
	}
	if stats != nil {
		j.Stats = stats
	}
	return j, nil
}

func (s *SQLiteStore) GetJob(id string) (*models.IndexJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	return j, ok
}

// Documents / FTS5
func (s *SQLiteStore) AddDocument(projectID, path, content string) *models.Document {
	// upsert document meta and chunked index in a transaction
	tx, err := s.db.Begin()
	if err != nil {
		return &models.Document{ID: "", ProjectID: projectID, Path: path}
	}
	defer tx.Rollback()

	id := s.nextID("doc")
	_, _ = tx.Exec(`INSERT OR REPLACE INTO documents(id,project_id,path,created_at) VALUES(?,?,?,?)`, id, projectID, path, time.Now().Format(time.RFC3339))
	chunks := chunkTextWithLines(content, 2000)
	now := time.Now().Format(time.RFC3339)
	for i, ch := range chunks {
		chkID := s.nextID("chk")
		_, _ = tx.Exec(`INSERT INTO chunks(id,doc_id,ord,text,token_count,start_line,end_line,created_at) VALUES(?,?,?,?,?,?,?,?)`, chkID, id, i, ch.Text, nil, ch.StartLine, ch.EndLine, now)
		_, _ = tx.Exec(`INSERT INTO termindex(doc_id,ord,text) VALUES(?,?,?)`, id, i, ch.Text)
	}
	_ = tx.Commit()
	return &models.Document{ID: id, ProjectID: projectID, Path: path}
}

// IncrementalStore implementation
func (s *SQLiteStore) UpsertDocument(projectID, path, content, sha, lang, mtime string) *models.Document {
	tx, err := s.db.Begin()
	if err != nil {
		return &models.Document{ID: "", ProjectID: projectID, Path: path}
	}
	defer tx.Rollback()

	// lookup existing document
	var existingID, existingSHA string
	var existingMTime string
	_ = tx.QueryRow(`SELECT id, sha, mtime FROM documents WHERE project_id=? AND path=?`, projectID, path).Scan(&existingID, &existingSHA, &existingMTime)
	now := time.Now().Format(time.RFC3339)
	if existingID == "" {
		// insert new document
		id := s.nextID("doc")
		_, _ = tx.Exec(`INSERT INTO documents(id,project_id,path,sha,lang,mtime,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, id, projectID, path, sha, lang, mtime, now, now)
		// index chunks (prefer code-aware when lang known)
		var chunks []chunk
		if lang == "go" || lang == "ts" || lang == "js" || lang == "py" {
			chunks = chunkSmartWithLines(content, lang, 2000)
		} else if lang == "md" || lang == "txt" {
			chunks = chunkDocWithLines(content, 2000)
		} else {
			chunks = chunkTextWithLines(content, 2000)
		}
		for i, ch := range chunks {
			chkID := s.nextID("chk")
			_, _ = tx.Exec(`INSERT INTO chunks(id,doc_id,ord,text,token_count,start_line,end_line,created_at) VALUES(?,?,?,?,?,?,?,?)`, chkID, id, i, ch.Text, nil, ch.StartLine, ch.EndLine, now)
			_, _ = tx.Exec(`INSERT INTO termindex(doc_id,ord,text) VALUES(?,?,?)`, id, i, ch.Text)
		}
		_ = tx.Commit()
		return &models.Document{ID: id, ProjectID: projectID, Path: path}
	}
	// if sha unchanged, skip reindex
	if (sha != "" && existingSHA == sha) || (mtime != "" && existingMTime == mtime) {
		_ = tx.Commit()
		return &models.Document{ID: existingID, ProjectID: projectID, Path: path}
	}
	// update sha/lang/updated_at
	_, _ = tx.Exec(`UPDATE documents SET sha=?, lang=?, mtime=?, updated_at=? WHERE id=?`, sha, lang, mtime, now, existingID)
	// reindex chunks: delete old entries then insert new
	_, _ = tx.Exec(`DELETE FROM termindex WHERE doc_id=?`, existingID)
	_, _ = tx.Exec(`DELETE FROM chunks WHERE doc_id=?`, existingID)
	var chunks []chunk
	if lang == "go" || lang == "ts" || lang == "js" || lang == "py" {
		chunks = chunkSmartWithLines(content, lang, 2000)
	} else if lang == "md" || lang == "txt" {
		chunks = chunkDocWithLines(content, 2000)
	} else {
		chunks = chunkTextWithLines(content, 2000)
	}
	for i, ch := range chunks {
		chkID := s.nextID("chk")
		_, _ = tx.Exec(`INSERT INTO chunks(id,doc_id,ord,text,token_count,start_line,end_line,created_at) VALUES(?,?,?,?,?,?,?,?)`, chkID, existingID, i, ch.Text, nil, ch.StartLine, ch.EndLine, now)
		_, _ = tx.Exec(`INSERT INTO termindex(doc_id,ord,text) VALUES(?,?,?)`, existingID, i, ch.Text)
	}
	_ = tx.Commit()
	return &models.Document{ID: existingID, ProjectID: projectID, Path: path}
}

// GetDocument returns a document metadata by project and path.
func (s *SQLiteStore) GetDocument(projectID, path string) (*models.Document, bool) {
	row := s.db.QueryRow(`SELECT id, project_id, path FROM documents WHERE project_id=? AND path=?`, projectID, path)
	var d models.Document
	if err := row.Scan(&d.ID, &d.ProjectID, &d.Path); err != nil {
		return nil, false
	}
	return &d, true
}

// DeleteDocument deletes a document and its chunks/index entries.
func (s *SQLiteStore) DeleteDocument(projectID, path string) error {
	return s.WithTx(func(tx *sql.Tx) error {
		var id string
		_ = tx.QueryRow(`SELECT id FROM documents WHERE project_id=? AND path=?`, projectID, path).Scan(&id)
		if id == "" {
			return nil
		}
		if _, err := tx.Exec(`DELETE FROM termindex WHERE doc_id=?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM chunks WHERE doc_id=?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM documents WHERE id=?`, id); err != nil {
			return err
		}
		return nil
	})
}

func (s *SQLiteStore) PruneDocuments(projectID string, present []string) error {
	// build set for quick lookup
	keep := make(map[string]struct{}, len(present))
	for _, p := range present {
		keep[p] = struct{}{}
	}
	// list existing documents for project
	rows, err := s.db.Query(`SELECT id,path FROM documents WHERE project_id=?`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var toDelete []string
	var ids []string
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err == nil {
			if _, ok := keep[path]; !ok {
				toDelete = append(toDelete, path)
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		_, _ = tx.Exec(`DELETE FROM termindex WHERE doc_id=?`, id)
		_, _ = tx.Exec(`DELETE FROM chunks WHERE doc_id=?`, id)
		_, _ = tx.Exec(`DELETE FROM documents WHERE id=?`, id)
	}
	return tx.Commit()
}

func (s *SQLiteStore) Search(projectID, query string, k int) []models.SearchResult {
	if k <= 0 {
		k = 10
	}
	// preview token window configurable via env
	prevTok := 10
	if v := os.Getenv("MYCODER_PREVIEW_SNIPPET_TOKENS"); v != "" {
		if n := atoiNoErr(v); n > 0 {
			prevTok = n
		}
	}
	var rows *sql.Rows
	var err error
	if projectID != "" {
		rows, err = s.db.Query(fmt.Sprintf(`
            SELECT d.path, bm25(termindex) as score, snippet(termindex, 2, '[', ']', ' … ', %d) as preview,
                   c.start_line, c.end_line
            FROM termindex
            JOIN documents d ON d.id = termindex.doc_id
            JOIN chunks c ON c.doc_id = termindex.doc_id AND c.ord = termindex.ord
            WHERE d.project_id = ? AND termindex MATCH ?
            ORDER BY score DESC LIMIT ?
        `, prevTok), projectID, query, k)
	} else {
		rows, err = s.db.Query(fmt.Sprintf(`
            SELECT d.path, bm25(termindex) as score, snippet(termindex, 2, '[', ']', ' … ', %d) as preview,
                   c.start_line, c.end_line
            FROM termindex
            JOIN documents d ON d.id = termindex.doc_id
            JOIN chunks c ON c.doc_id = termindex.doc_id AND c.ord = termindex.ord
            WHERE termindex MATCH ?
            ORDER BY score DESC LIMIT ?
        `, prevTok), query, k)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []models.SearchResult
	for rows.Next() {
		var path, preview string
		var score float64
		var start, end sql.NullInt64
		if err := rows.Scan(&path, &score, &preview, &start, &end); err == nil {
			res := models.SearchResult{Path: path, Score: score, Preview: preview}
			if start.Valid {
				res.StartLine = int(start.Int64)
			}
			if end.Valid {
				res.EndLine = int(end.Int64)
			}
			out = append(out, res)
		}
	}
	return out
}

// UpsertSymbols replaces symbols for a given project+path with the provided set.
func (s *SQLiteStore) UpsertSymbols(projectID, path, lang string, symbols []models.Symbol) error {
	return s.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM symbols WHERE project_id=? AND path=?`, projectID, path); err != nil {
			return err
		}
		for _, sym := range symbols {
			_, err := tx.Exec(`INSERT INTO symbols(id,project_id,path,lang,name,kind,start_line,end_line,signature,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				s.nextID("sym"), projectID, path, lang, sym.Name, sym.Kind, sym.StartLine, sym.EndLine, sym.Signature, time.Now().Format(time.RFC3339))
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// UpsertSymbolEdges replaces edges for a given project+path.
func (s *SQLiteStore) UpsertSymbolEdges(projectID, path string, edges []models.SymbolEdge) error {
	return s.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM symbol_edges WHERE project_id=? AND path=?`, projectID, path); err != nil {
			return err
		}
		for _, e := range edges {
			if e.Kind == "" {
				e.Kind = "ref"
			}
			_, err := tx.Exec(`INSERT INTO symbol_edges(id,project_id,path,src_name,dst_name,kind,created_at) VALUES(?,?,?,?,?,?,?)`,
				s.nextID("sedge"), projectID, path, e.SrcName, e.DstName, e.Kind, time.Now().Format(time.RFC3339))
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// ListSymbolEdges lists edges for a project (optionally filtered by path).
func (s *SQLiteStore) ListSymbolEdges(projectID, path string) ([]models.SymbolEdge, error) {
	var rows *sql.Rows
	var err error
	if path != "" {
		rows, err = s.db.Query(`SELECT id, path, src_name, dst_name, kind FROM symbol_edges WHERE project_id=? AND path=? ORDER BY id`, projectID, path)
	} else {
		rows, err = s.db.Query(`SELECT id, path, src_name, dst_name, kind FROM symbol_edges WHERE project_id=? ORDER BY path,id`, projectID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SymbolEdge
	for rows.Next() {
		var e models.SymbolEdge
		if err := rows.Scan(&e.ID, &e.Path, &e.SrcName, &e.DstName, &e.Kind); err == nil {
			e.ProjectID = projectID
			out = append(out, e)
		}
	}
	return out, nil
}

// chunkText splits text into near-maxLen character chunks at newline boundaries when possible.
func chunkText(s string, maxLen int) []string {
	if maxLen <= 0 {
		maxLen = 2000
	}
	if len(s) <= maxLen {
		return []string{s}
	}
	var out []string
	start := 0
	for start < len(s) {
		end := start + maxLen
		if end >= len(s) {
			out = append(out, s[start:])
			break
		}
		cut := end
		// try to break at newline within a small window
		for i := end; i > start && i > end-200; i-- {
			if s[i-1] == '\n' {
				cut = i
				break
			}
		}
		out = append(out, s[start:cut])
		start = cut
	}
	return out
}

type chunk struct {
	Text      string
	StartLine int
	EndLine   int
}

// chunkTextWithLines splits text and tracks line ranges for each chunk.
func chunkTextWithLines(s string, maxLen int) []chunk {
	if len(s) == 0 {
		return nil
	}
	maxTok, overlap := chunkConfig(maxLen)
	return splitTokensWithOverlap(s, maxTok, overlap, 1)
}

// chunkSmartWithLines prefers code boundaries when possible based on language.
func chunkSmartWithLines(s, lang string, maxLen int) []chunk {
	if len(s) == 0 {
		return nil
	}
	re := boundaryRegex(lang)
	lines := strings.Split(s, "\n")
	var pieces []chunk
	var buf strings.Builder
	startLine := 1
	// Collect code-aware pieces first
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if re != nil && re.MatchString(line) && buf.Len() >= 1_000 { // approx boundary size
			text := buf.String()
			if text != "" {
				pieces = append(pieces, chunk{Text: text, StartLine: startLine, EndLine: startLine + strings.Count(text, "\n")})
				startLine += strings.Count(text, "\n")
				buf.Reset()
			}
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	if buf.Len() > 0 {
		text := buf.String()
		pieces = append(pieces, chunk{Text: text, StartLine: startLine, EndLine: startLine + strings.Count(text, "\n")})
	}
	// Now apply token windows with overlap per piece
	maxTok, overlap := chunkConfig(maxLen)
	var out []chunk
	for _, p := range pieces {
		subs := splitTokensWithOverlap(p.Text, maxTok, overlap, p.StartLine)
		out = append(out, subs...)
	}
	return out
}

func boundaryRegex(lang string) *regexp.Regexp {
	switch lang {
	case "go":
		return regexp.MustCompile(`^(func|type|const|var)\b`)
	case "ts", "js":
		return regexp.MustCompile(`^(export\s+)?(async\s+)?(function|class)\b`)
	case "py":
		return regexp.MustCompile(`^(def|class)\b`)
	default:
		return nil
	}
}

// chunkDocWithLines splits markdown/text into chunks by headings and paragraph
// boundaries while respecting a soft maxLen. Headings always start a new chunk.
func chunkDocWithLines(s string, maxLen int) []chunk {
	if len(s) == 0 {
		return nil
	}
	lines := strings.Split(s, "\n")
	var pieces []chunk
	var buf strings.Builder
	startLine := 1
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		text := buf.String()
		pieces = append(pieces, chunk{Text: text, StartLine: startLine, EndLine: startLine + strings.Count(text, "\n")})
		startLine += strings.Count(text, "\n")
		buf.Reset()
	}
	isHeading := func(l string) bool {
		ltrim := strings.TrimSpace(l)
		return strings.HasPrefix(ltrim, "#")
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if isHeading(line) {
			flush()
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
		if strings.TrimSpace(line) == "" && buf.Len() >= 1000 {
			flush()
		}
	}
	flush()
	// apply token windows per piece
	maxTok, overlap := chunkConfig(maxLen)
	var out []chunk
	for _, p := range pieces {
		subs := splitTokensWithOverlap(p.Text, maxTok, overlap, p.StartLine)
		out = append(out, subs...)
	}
	return out
}

// --- token-based chunking with overlap ---
type tokenPos struct{ start, end int }

func scanTokens(s string) []tokenPos {
	var toks []tokenPos
	n := len(s)
	i := 0
	for i < n {
		// skip whitespace
		for i < n {
			c := s[i]
			if c == ' ' || c == '\n' || c == '\t' || c == '\r' {
				i++
				continue
			}
			break
		}
		if i >= n {
			break
		}
		st := i
		for i < n {
			c := s[i]
			if c == ' ' || c == '\n' || c == '\t' || c == '\r' {
				break
			}
			i++
		}
		toks = append(toks, tokenPos{start: st, end: i})
	}
	return toks
}

func splitTokensWithOverlap(s string, maxTokens int, overlapRatio float64, startLine int) []chunk {
	if maxTokens <= 0 {
		maxTokens = 400
	}
	if overlapRatio < 0 {
		overlapRatio = 0
	}
	if overlapRatio > 0.5 {
		overlapRatio = 0.5
	}
	toks := scanTokens(s)
	if len(toks) == 0 {
		return nil
	}
	step := maxTokens - int(float64(maxTokens)*overlapRatio+0.5)
	if step < 1 {
		step = 1
	}
	var out []chunk
	// prefix newline counts for fast line offsets
	// compute lines up to any byte index by scanning prefix each time (acceptable for tests scale)
	for i := 0; i < len(toks); i += step {
		j := i + maxTokens
		if j > len(toks) {
			j = len(toks)
		}
		st := toks[i].start
		en := toks[j-1].end
		piece := s[st:en]
		// compute line offsets
		prefix := s[:st]
		start := startLine + strings.Count(prefix, "\n")
		end := start + strings.Count(piece, "\n")
		out = append(out, chunk{Text: piece, StartLine: start, EndLine: end})
		if j == len(toks) {
			break
		}
	}
	return out
}

func chunkConfig(hint int) (maxTokens int, overlap float64) {
	// env override
	if v := os.Getenv("MYCODER_CHUNK_MAX_TOKENS"); v != "" {
		if n := atoiNoErr(v); n > 0 {
			maxTokens = n
		}
	}
	if maxTokens == 0 {
		if hint > 0 {
			maxTokens = hint / 5
		} // rough mapping from old char limit
		if maxTokens <= 0 {
			maxTokens = 400
		}
	}
	overlap = 0.10
	if v := os.Getenv("MYCODER_CHUNK_OVERLAP_RATIO"); v != "" {
		if f := atofNoErr(v); f >= 0 && f <= 0.5 {
			overlap = f
		}
	}
	return
}

func atoiNoErr(s string) int {
	n := 0
	sign := 1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 0 && (c == '-' || c == '+') {
			if c == '-' {
				sign = -1
			}
			continue
		}
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return sign * n
}

func atofNoErr(s string) float64 {
	// very small parser: int or decimal like 0.15
	whole, frac := 0, 0
	fdiv := 1
	sign := 1
	i := 0
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		if s[i] == '-' {
			sign = -1
		}
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		whole = whole*10 + int(s[i]-'0')
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			frac = frac*10 + int(s[i]-'0')
			fdiv *= 10
			i++
		}
	}
	return float64(sign) * (float64(whole) + float64(frac)/float64(fdiv))
}

func (s *SQLiteStore) Stats() map[string]int {
	// best-effort counts
	count := func(q string) int {
		row := s.db.QueryRow(q)
		var n int
		_ = row.Scan(&n)
		return n
	}
	s.mu.Lock()
	jobs := len(s.jobs)
	s.mu.Unlock()
	return map[string]int{
		"projects":  count("SELECT COUNT(1) FROM projects"),
		"documents": count("SELECT COUNT(1) FROM documents"),
		"jobs":      jobs,
	}
}

// Knowledge minimal operations
func (s *SQLiteStore) AddKnowledge(projectID, sourceType, pathOrURL, title, text string, trust float64, pinned bool) (*models.Knowledge, error) {
	id := s.nextID("kn")
	_, err := s.db.Exec(`INSERT INTO knowledge(id,project_id,source_type,path_or_url,title,text,trust_score,pinned,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, projectID, sourceType, pathOrURL, title, text, trust, boolToInt(pinned), time.Now().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return &models.Knowledge{ID: id, ProjectID: projectID, SourceType: sourceType, PathOrURL: pathOrURL, Title: title, Text: text, TrustScore: trust, Pinned: pinned}, nil
}

func (s *SQLiteStore) ListKnowledge(projectID string, minScore float64) ([]*models.Knowledge, error) {
	rows, err := s.db.Query(`SELECT id,source_type,path_or_url,title,text,trust_score,pinned FROM knowledge WHERE project_id=? AND trust_score>=? ORDER BY trust_score DESC, created_at DESC`, projectID, minScore)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Knowledge
	for rows.Next() {
		var k models.Knowledge
		var pinned int
		if err := rows.Scan(&k.ID, &k.SourceType, &k.PathOrURL, &k.Title, &k.Text, &k.TrustScore, &pinned); err == nil {
			k.ProjectID = projectID
			k.Pinned = pinned == 1
			out = append(out, &k)
		}
	}
	return out, nil
}

func (s *SQLiteStore) VetKnowledge(projectID string) (int, error) {
	// improved vet scoring: text length, pinned boost, freshness boost; clamp at 1.0
	res, err := s.db.Exec(`
        UPDATE knowledge
        SET trust_score = MIN(1.0,
            trust_score
            + CASE WHEN length(text) >= 200 THEN 0.05 ELSE 0.02 END
            + CASE WHEN pinned = 1 THEN 0.05 ELSE 0.00 END
            + CASE WHEN (julianday('now') - julianday(COALESCE(verified_at, created_at))) < 7 THEN 0.03 ELSE 0.00 END
        ),
        verified_at = CURRENT_TIMESTAMP
        WHERE project_id = ?
    `, projectID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) PromoteKnowledge(projectID, title, text, pathOrURL, commitSHA, filesCSV, symbolsCSV string, pin bool) (*models.Knowledge, error) {
	id := s.nextID("kn")
	_, err := s.db.Exec(`INSERT INTO knowledge(id,project_id,source_type,path_or_url,title,text,trust_score,pinned,commit_sha,files,symbols,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, projectID, "code", pathOrURL, title, text, 0.7, boolToInt(pin), commitSHA, filesCSV, symbolsCSV, time.Now().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return &models.Knowledge{ID: id, ProjectID: projectID, SourceType: "code", PathOrURL: pathOrURL, Title: title, Text: text, TrustScore: 0.7, Pinned: pin, CommitSHA: commitSHA, Files: filesCSV, Symbols: symbolsCSV}, nil
}

func (s *SQLiteStore) ReverifyKnowledge(projectID string) (int, error) {
	res, err := s.db.Exec(`UPDATE knowledge SET trust_score = trust_score + 0.05, verified_at=? WHERE project_id=?`, time.Now().Format(time.RFC3339), projectID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) GCKnowledge(projectID string, minScore float64) (int, error) {
	res, err := s.db.Exec(`DELETE FROM knowledge WHERE project_id=? AND pinned=0 AND trust_score < ?`, projectID, minScore)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// GCKnowledgeTTL removes non-pinned knowledge items whose tags.ttlUntil is in the past.
// Since SQLite JSON1 may not be available, we parse tags in Go and delete expired rows.
func (s *SQLiteStore) GCKnowledgeTTL(projectID string) (int, error) {
	rows, err := s.db.Query(`SELECT id, tags FROM knowledge WHERE project_id=? AND pinned=0 AND tags IS NOT NULL`, projectID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type rec struct{ id, tags string }
	var expired []string
	for rows.Next() {
		var id, tags string
		if err := rows.Scan(&id, &tags); err == nil {
			// naive json parse into map[string]string
			var m map[string]string
			if err := json.Unmarshal([]byte(tags), &m); err == nil {
				if until, ok := m["ttlUntil"]; ok && until != "" {
					if t, err := time.Parse(time.RFC3339, until); err == nil && time.Now().After(t) {
						expired = append(expired, id)
					}
				}
			}
		}
	}
	if len(expired) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, id := range expired {
		_, _ = tx.Exec(`DELETE FROM knowledge WHERE id=?`, id)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(expired), nil
}

// DecayKnowledge reduces trust_score by rate for non-pinned items older than afterDays since last verify/create.
func (s *SQLiteStore) DecayKnowledge(projectID string, rate float64, afterDays int) (int, error) {
	if rate <= 0 {
		return 0, nil
	}
	// SQLite: clamp at 0.0, only apply to non-pinned and older than afterDays
	q := `UPDATE knowledge
          SET trust_score = MAX(0.0, trust_score - ?)
          WHERE project_id=? AND pinned=0 AND (julianday('now') - julianday(COALESCE(verified_at, created_at))) >= ?`
	res, err := s.db.Exec(q, rate, projectID, afterDays)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CleanupConversations deletes non-pinned conversations older than ttlDays and their messages/summaries.
func (s *SQLiteStore) CleanupConversations(ttlDays int) (int, error) {
	if ttlDays <= 0 {
		return 0, nil
	}
	// select old, non-pinned conversation ids
	rows, err := s.db.Query(`SELECT id FROM conversations WHERE pinned=0 AND (julianday('now') - julianday(COALESCE(updated_at, created_at))) >= ?`, ttlDays)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return 0, nil
	}
	// delete with transaction
	return s.cleanupConversationIDs(ids)
}

func (s *SQLiteStore) cleanupConversationIDs(ids []string) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, id := range ids {
		_, _ = tx.Exec(`DELETE FROM conversation_messages WHERE conv_id=?`, id)
		_, _ = tx.Exec(`DELETE FROM conversation_summaries WHERE conv_id=?`, id)
		_, _ = tx.Exec(`DELETE FROM conversations WHERE id=?`, id)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (s *SQLiteStore) ApproveKnowledge(projectID string, ids []string, pin bool, minTrust float64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	n := 0
	for _, id := range ids {
		_, err := s.db.Exec(`UPDATE knowledge SET pinned = CASE WHEN ? THEN 1 ELSE pinned END, trust_score = CASE WHEN trust_score < ? THEN ? ELSE trust_score END WHERE project_id=? AND id=?`, pin, minTrust, minTrust, projectID, id)
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
