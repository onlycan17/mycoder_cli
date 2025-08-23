package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestGCKnowledgeTTL(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "kn.db")
	s, err := NewSQLite(dbpath)
	if err != nil {
		t.Skip("sqlite not available:", err)
	}
	// insert two knowledge items: one expired, one future TTL
	past := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	future := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	// expired
	var tags1 = map[string]string{"ttlUntil": past}
	tb1, _ := json.Marshal(tags1)
	id1 := s.nextID("kn")
	_, _ = s.db.Exec(`INSERT INTO knowledge(id,project_id,source_type,path_or_url,title,text,trust_score,pinned,tags,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		id1, "p1", "web", "http://e/1", "t1", "x", 0.1, 0, string(tb1), time.Now().Format(time.RFC3339))
	// not expired
	var tags2 = map[string]string{"ttlUntil": future}
	tb2, _ := json.Marshal(tags2)
	id2 := s.nextID("kn")
	_, _ = s.db.Exec(`INSERT INTO knowledge(id,project_id,source_type,path_or_url,title,text,trust_score,pinned,tags,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		id2, "p1", "web", "http://e/2", "t2", "y", 0.1, 0, string(tb2), time.Now().Format(time.RFC3339))

	n, err := s.GCKnowledgeTTL("p1")
	if err != nil {
		t.Fatalf("gc ttl: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 removed, got %d", n)
	}
}
