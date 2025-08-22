package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupConversationsTTL(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "conv.db")
	s, err := NewSQLite(dbpath)
	if err != nil {
		t.Skip("sqlite not available:", err)
	}

	// insert two conversations: one old (unpinned), one recent (pinned)
	oldID := s.nextID("conv")
	now := time.Now()
	oldCreated := now.AddDate(0, 0, -40).Format(time.RFC3339)
	_, _ = s.db.Exec(`INSERT INTO conversations(id,project_id,title,pinned,created_at,updated_at) VALUES(?,?,?,?,?,?)`, oldID, "p1", "old", 0, oldCreated, oldCreated)
	_, _ = s.db.Exec(`INSERT INTO conversation_messages(id,conv_id,role,content,created_at) VALUES(?,?,?,?,?)`, s.nextID("msg"), oldID, "user", "hello", oldCreated)

	recentID := s.nextID("conv")
	_, _ = s.db.Exec(`INSERT INTO conversations(id,project_id,title,pinned,created_at,updated_at) VALUES(?,?,?,?,?,?)`, recentID, "p1", "recent", 1, now.Format(time.RFC3339), now.Format(time.RFC3339))
	_, _ = s.db.Exec(`INSERT INTO conversation_messages(id,conv_id,role,content,created_at) VALUES(?,?,?,?,?)`, s.nextID("msg"), recentID, "user", "hi", now.Format(time.RFC3339))

	n, err := s.CleanupConversations(30)
	if err != nil {
		t.Fatalf("cleanup error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 conversation deleted, got %d", n)
	}
	// ensure old conv is gone, recent stays
	var cnt int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM conversations WHERE id=?`, oldID).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("expected old conversation removed")
	}
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM conversations WHERE id=?`, recentID).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("expected recent pinned conversation to remain")
	}
}
