package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
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
	// migrate (method call on composite literal requires parentheses)
	if err := (sqlm.Migrator{}).Up(context.Background(), db); err != nil {
		return nil, err
	}
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
func (s *SQLiteStore) UpsertDocument(projectID, path, content, sha, lang string) *models.Document {
	tx, err := s.db.Begin()
	if err != nil {
		return &models.Document{ID: "", ProjectID: projectID, Path: path}
	}
	defer tx.Rollback()

	// lookup existing document
	var existingID, existingSHA string
	_ = tx.QueryRow(`SELECT id, sha FROM documents WHERE project_id=? AND path=?`, projectID, path).Scan(&existingID, &existingSHA)
	now := time.Now().Format(time.RFC3339)
	if existingID == "" {
		// insert new document
		id := s.nextID("doc")
		_, _ = tx.Exec(`INSERT INTO documents(id,project_id,path,sha,lang,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, id, projectID, path, sha, lang, now, now)
		// index chunks
		chunks := chunkTextWithLines(content, 2000)
		for i, ch := range chunks {
			chkID := s.nextID("chk")
			_, _ = tx.Exec(`INSERT INTO chunks(id,doc_id,ord,text,token_count,start_line,end_line,created_at) VALUES(?,?,?,?,?,?,?,?)`, chkID, id, i, ch.Text, nil, ch.StartLine, ch.EndLine, now)
			_, _ = tx.Exec(`INSERT INTO termindex(doc_id,ord,text) VALUES(?,?,?)`, id, i, ch.Text)
		}
		_ = tx.Commit()
		return &models.Document{ID: id, ProjectID: projectID, Path: path}
	}
	// if sha unchanged, skip reindex
	if sha != "" && existingSHA == sha {
		_ = tx.Commit()
		return &models.Document{ID: existingID, ProjectID: projectID, Path: path}
	}
	// update sha/lang/updated_at
	_, _ = tx.Exec(`UPDATE documents SET sha=?, lang=?, updated_at=? WHERE id=?`, sha, lang, now, existingID)
	// reindex chunks: delete old entries then insert new
	_, _ = tx.Exec(`DELETE FROM termindex WHERE doc_id=?`, existingID)
	_, _ = tx.Exec(`DELETE FROM chunks WHERE doc_id=?`, existingID)
	chunks := chunkTextWithLines(content, 2000)
	for i, ch := range chunks {
		chkID := s.nextID("chk")
		_, _ = tx.Exec(`INSERT INTO chunks(id,doc_id,ord,text,token_count,start_line,end_line,created_at) VALUES(?,?,?,?,?,?,?,?)`, chkID, existingID, i, ch.Text, nil, ch.StartLine, ch.EndLine, now)
		_, _ = tx.Exec(`INSERT INTO termindex(doc_id,ord,text) VALUES(?,?,?)`, existingID, i, ch.Text)
	}
	_ = tx.Commit()
	return &models.Document{ID: existingID, ProjectID: projectID, Path: path}
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
	var rows *sql.Rows
	var err error
	if projectID != "" {
		rows, err = s.db.Query(`
            SELECT d.path, bm25(termindex) as score, snippet(termindex, 2, '[', ']', ' … ', 10) as preview,
                   c.start_line, c.end_line
            FROM termindex
            JOIN documents d ON d.id = termindex.doc_id
            JOIN chunks c ON c.doc_id = termindex.doc_id AND c.ord = termindex.ord
            WHERE d.project_id = ? AND termindex MATCH ?
            ORDER BY score DESC LIMIT ?
        `, projectID, query, k)
	} else {
		rows, err = s.db.Query(`
            SELECT d.path, bm25(termindex) as score, snippet(termindex, 2, '[', ']', ' … ', 10) as preview,
                   c.start_line, c.end_line
            FROM termindex
            JOIN documents d ON d.id = termindex.doc_id
            JOIN chunks c ON c.doc_id = termindex.doc_id AND c.ord = termindex.ord
            WHERE termindex MATCH ?
            ORDER BY score DESC LIMIT ?
        `, query, k)
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
	if maxLen <= 0 {
		maxLen = 2000
	}
	if len(s) == 0 {
		return nil
	}
	// Precompute line breaks
	// We’ll count lines as we segment
	start := 0
	currentLine := 1
	var out []chunk
	for start < len(s) {
		end := start + maxLen
		if end >= len(s) {
			end = len(s)
		}
		cut := end
		for i := end; i > start && i > end-200; i-- {
			if s[i-1] == '\n' {
				cut = i
				break
			}
		}
		if cut == start {
			cut = end
		}
		piece := s[start:cut]
		lines := 1
		for i := 0; i < len(piece); i++ {
			if piece[i] == '\n' {
				lines++
			}
		}
		c := chunk{Text: piece, StartLine: currentLine, EndLine: currentLine + lines - 1}
		out = append(out, c)
		currentLine += lines - 1
		start = cut
	}
	return out
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

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
