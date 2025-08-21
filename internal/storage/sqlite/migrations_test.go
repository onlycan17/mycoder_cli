package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrationsVersioningAndTables(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "mig.db")
	db, err := sql.Open("sqlite", dbpath)
	if err != nil {
		t.Skip("sqlite open:", err)
	}
	defer db.Close()

	m := Manager{}
	if err := m.UpToLatest(context.Background(), db); err != nil {
		t.Fatalf("UpToLatest error: %v", err)
	}
	// version should be > 0
	var v int
	if err := db.QueryRow(`SELECT version FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatalf("version scan: %v", err)
	}
	if v <= 0 {
		t.Fatalf("unexpected version: %d", v)
	}

	// ensure v3 tables exist (embeddings/symbols/patches) by querying sqlite_master
	mustHave := []string{"embeddings", "symbols", "patches"}
	for _, name := range mustHave {
		var cnt int
		if err := db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&cnt); err != nil || cnt == 0 {
			t.Fatalf("expected table %s to exist", name)
		}
	}

	// down one (if possible) then back up
	_ = m.DownOne(context.Background(), db)
	if err := m.UpToLatest(context.Background(), db); err != nil {
		t.Fatalf("UpToLatest after down error: %v", err)
	}
}
