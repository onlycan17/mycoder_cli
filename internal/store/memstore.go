package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"mycoder/internal/models"
)

type Store struct {
	mu       sync.RWMutex
	projects map[string]*models.Project
	jobs     map[string]*models.IndexJob
	docs     map[string]*models.Document
	byPath   map[string]string // projectID+":"+path -> docID
	seq      int64
	// knowledge minimal in-memory
	knowledge []*models.Knowledge
}

func New() *Store {
	return &Store{
		projects:  make(map[string]*models.Project),
		jobs:      make(map[string]*models.IndexJob),
		docs:      make(map[string]*models.Document),
		byPath:    make(map[string]string),
		knowledge: []*models.Knowledge{},
	}
}

func (s *Store) nextID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s-%d", prefix, s.seq)
}

// Projects
func (s *Store) CreateProject(name, root string, ignore []string) *models.Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID("proj")
	p := &models.Project{ID: id, Name: name, RootPath: root, Ignore: ignore, Created: time.Now()}
	s.projects[id] = p
	return p
}

func (s *Store) ListProjects() []*models.Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*models.Project, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, p)
	}
	return out
}

func (s *Store) GetProject(id string) (*models.Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.projects[id]
	return p, ok
}

// Index Jobs
func (s *Store) CreateIndexJob(projectID string, mode models.IndexMode) (*models.IndexJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[projectID]; !ok {
		return nil, errors.New("project not found")
	}
	id := s.nextID("job")
	j := &models.IndexJob{ID: id, ProjectID: projectID, Mode: mode, Status: models.JobPending, StartedAt: time.Now()}
	s.jobs[id] = j
	return j, nil
}

func (s *Store) SetJobStatus(id string, st models.IndexJobStatus, stats map[string]int) (*models.IndexJob, error) {
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

func (s *Store) GetJob(id string) (*models.IndexJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

// Documents (for in-memory search/demo)
func (s *Store) AddDocument(projectID, path, content string) *models.Document {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := projectID + ":" + path
	if id, ok := s.byPath[key]; ok {
		d := s.docs[id]
		d.Content = content
		return d
	}
	id := s.nextID("doc")
	d := &models.Document{ID: id, ProjectID: projectID, Path: path, Content: content}
	s.docs[id] = d
	s.byPath[key] = id
	return d
}

func (s *Store) Search(projectID, query string, k int) []models.SearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	type scored struct{ res models.SearchResult }
	var out []scored
	q := strings.ToLower(query)
	for _, d := range s.docs {
		if projectID != "" && d.ProjectID != projectID {
			continue
		}
		text := strings.ToLower(d.Content)
		idx := strings.Index(text, q)
		if idx < 0 {
			continue
		}
		score := 1.0
		count := 0
		off := 0
		for {
			i := strings.Index(text[off:], q)
			if i < 0 {
				break
			}
			count++
			off += i + len(q)
		}
		if count > 1 {
			score += float64(count-1) * 0.25
		}
		startLine, endLine := 1, 1
		for i := 0; i < idx && i < len(d.Content); i++ {
			if d.Content[i] == '\n' {
				startLine++
			}
		}
		endLine = startLine
		lines := strings.Split(d.Content, "\n")
		prev := ""
		if startLine-1 >= 0 && startLine-1 < len(lines) {
			prev = strings.TrimSpace(lines[startLine-1])
		}
		out = append(out, scored{res: models.SearchResult{Path: d.Path, Score: score, Preview: prev, StartLine: startLine, EndLine: endLine}})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].res.Score == out[j].res.Score {
			return out[i].res.Path < out[j].res.Path
		}
		return out[i].res.Score > out[j].res.Score
	})
	if k <= 0 || k > len(out) {
		k = len(out)
	}
	res := make([]models.SearchResult, 0, k)
	for i := 0; i < k; i++ {
		res = append(res, out[i].res)
	}
	return res
}

// Incremental helpers (sha/lang ignored)
func (s *Store) UpsertDocument(projectID, path, content, sha, lang string) *models.Document {
	return s.AddDocument(projectID, path, content)
}

func (s *Store) PruneDocuments(projectID string, present []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	presentSet := make(map[string]struct{}, len(present))
	for _, p := range present {
		presentSet[projectID+":"+p] = struct{}{}
	}
	for key, id := range s.byPath {
		if !strings.HasPrefix(key, projectID+":") {
			continue
		}
		if _, ok := presentSet[key]; !ok {
			delete(s.byPath, key)
			delete(s.docs, id)
		}
	}
	return nil
}

func (s *Store) Stats() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]int{
		"projects":  len(s.projects),
		"jobs":      len(s.jobs),
		"documents": len(s.docs),
		"knowledge": len(s.knowledge),
	}
}

// Knowledge in-memory minimal
func (s *Store) AddKnowledge(projectID, sourceType, pathOrURL, title, text string, trust float64, pinned bool) (*models.Knowledge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID("kn")
	k := &models.Knowledge{ID: id, ProjectID: projectID, SourceType: sourceType, PathOrURL: pathOrURL, Title: title, Text: text, TrustScore: trust, Pinned: pinned}
	s.knowledge = append(s.knowledge, k)
	return k, nil
}

func (s *Store) ListKnowledge(projectID string, minScore float64) ([]*models.Knowledge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*models.Knowledge
	for _, k := range s.knowledge {
		if k.ProjectID == projectID && k.TrustScore >= minScore {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *Store) VetKnowledge(projectID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range s.knowledge {
		if k.ProjectID == projectID && len(k.Text) > 0 {
			k.TrustScore += 0.1
			n++
		}
	}
	return n, nil
}

func (s *Store) PromoteKnowledge(projectID, title, text, pathOrURL, commitSHA, filesCSV, symbolsCSV string, pin bool) (*models.Knowledge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID("kn")
	k := &models.Knowledge{ID: id, ProjectID: projectID, SourceType: "code", Title: title, Text: text, PathOrURL: pathOrURL, CommitSHA: commitSHA, Files: filesCSV, Symbols: symbolsCSV, TrustScore: 0.7, Pinned: pin}
	s.knowledge = append(s.knowledge, k)
	return k, nil
}

func (s *Store) ReverifyKnowledge(projectID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range s.knowledge {
		if k.ProjectID == projectID {
			k.TrustScore += 0.05
			n++
		}
	}
	return n, nil
}

func (s *Store) GCKnowledge(projectID string, minScore float64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.knowledge[:0]
	removed := 0
	for _, k := range s.knowledge {
		if k.ProjectID == projectID && !k.Pinned && k.TrustScore < minScore {
			removed++
			continue
		}
		kept = append(kept, k)
	}
	s.knowledge = kept
	return removed, nil
}
