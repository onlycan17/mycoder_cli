package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Manager handles schema versioning and basic seeding.
type Manager struct{}

const latestVersion = 3

func (m Manager) ensureTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER NOT NULL);`)
	if err != nil {
		return err
	}
	// initialize row if empty
	var cnt int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations`).Scan(&cnt)
	if cnt == 0 {
		_, err = db.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(0)`)
	}
	return err
}

func (m Manager) version(ctx context.Context, db *sql.DB) (int, error) {
	if err := m.ensureTable(ctx, db); err != nil {
		return 0, err
	}
	var v int
	if err := db.QueryRowContext(ctx, `SELECT version FROM schema_migrations`).Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

func (m Manager) setVersion(ctx context.Context, db *sql.DB, v int) error {
	_, err := db.ExecContext(ctx, `UPDATE schema_migrations SET version=?`, v)
	return err
}

// UpToLatest applies migrations to reach latestVersion.
func (m Manager) UpToLatest(ctx context.Context, db *sql.DB) error {
	cur, err := m.version(ctx, db)
	if err != nil {
		return err
	}
	for v := cur + 1; v <= latestVersion; v++ {
		if err := m.up(ctx, db, v); err != nil {
			return fmt.Errorf("migrate up to v%d: %w", v, err)
		}
		if err := m.setVersion(ctx, db, v); err != nil {
			return err
		}
	}
	return nil
}

// DownOne attempts to roll back the last migration if supported.
func (m Manager) DownOne(ctx context.Context, db *sql.DB) error {
	cur, err := m.version(ctx, db)
	if err != nil {
		return err
	}
	if cur <= 0 {
		return nil
	}
	if err := m.down(ctx, db, cur); err != nil {
		return err
	}
	return m.setVersion(ctx, db, cur-1)
}

func (m Manager) up(ctx context.Context, db *sql.DB, v int) error {
	switch v {
	case 1:
		return (Migrator{}).Up(ctx, db)
	case 2:
		// additive columns already included in Migrator.Up best-effort; ensure presence
		stmts := []string{
			`ALTER TABLE chunks ADD COLUMN start_line INTEGER`,
			`ALTER TABLE chunks ADD COLUMN end_line INTEGER`,
			`ALTER TABLE documents ADD COLUMN mtime TEXT`,
			`ALTER TABLE knowledge ADD COLUMN commit_sha TEXT`,
			`ALTER TABLE knowledge ADD COLUMN files TEXT`,
			`ALTER TABLE knowledge ADD COLUMN symbols TEXT`,
			`ALTER TABLE knowledge ADD COLUMN tags TEXT`,
		}
		for i, s := range stmts {
			_, _ = db.ExecContext(ctx, s)
			_ = i
		}
		return nil
	case 3:
		// embeddings, patches, symbols
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS embeddings (
                id TEXT PRIMARY KEY,
                project_id TEXT NOT NULL,
                doc_id TEXT,
                chunk_id TEXT,
                provider TEXT,
                model TEXT,
                dim INTEGER,
                vector TEXT,
                created_at TEXT NOT NULL,
                FOREIGN KEY(project_id) REFERENCES projects(id),
                FOREIGN KEY(doc_id) REFERENCES documents(id),
                FOREIGN KEY(chunk_id) REFERENCES chunks(id)
            );`,
			`CREATE INDEX IF NOT EXISTS idx_embeddings_project_doc_chunk ON embeddings(project_id, doc_id, chunk_id);`,
			`CREATE TABLE IF NOT EXISTS patches (
                id TEXT PRIMARY KEY,
                project_id TEXT NOT NULL,
                path TEXT NOT NULL,
                hunks TEXT NOT NULL,
                applied INTEGER DEFAULT 0,
                created_at TEXT NOT NULL,
                applied_at TEXT,
                FOREIGN KEY(project_id) REFERENCES projects(id)
            );`,
			`CREATE INDEX IF NOT EXISTS idx_patches_project_path ON patches(project_id, path);`,
			`CREATE TABLE IF NOT EXISTS symbols (
                id TEXT PRIMARY KEY,
                project_id TEXT NOT NULL,
                path TEXT NOT NULL,
                lang TEXT,
                name TEXT NOT NULL,
                kind TEXT,
                start_line INTEGER,
                end_line INTEGER,
                signature TEXT,
                created_at TEXT NOT NULL,
                FOREIGN KEY(project_id) REFERENCES projects(id)
            );`,
			`CREATE INDEX IF NOT EXISTS idx_symbols_project_name ON symbols(project_id, name);`,
		}
		for i, s := range stmts {
			if _, err := db.ExecContext(ctx, s); err != nil {
				return fmt.Errorf("v3 step %d: %w", i, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown migration version %d", v)
	}
}

func (m Manager) down(ctx context.Context, db *sql.DB, v int) error {
	switch v {
	case 3:
		// drop additive tables
		stmts := []string{
			`DROP TABLE IF EXISTS symbols;`,
			`DROP TABLE IF EXISTS patches;`,
			`DROP TABLE IF EXISTS embeddings;`,
		}
		for _, s := range stmts {
			_, _ = db.ExecContext(ctx, s)
		}
		return nil
	case 2:
		// dropping columns in SQLite requires table rebuild; not supported here
		return errors.New("down from v2 not supported")
	case 1:
		return errors.New("down from v1 not supported")
	default:
		return fmt.Errorf("unknown migration version %d", v)
	}
}

// Seed inserts minimal seed data when enabled via env (MYCODER_DB_SEED=true/1)
func (m Manager) Seed(ctx context.Context, db *sql.DB) error {
	v := strings.ToLower(os.Getenv("MYCODER_DB_SEED"))
	if v == "" || v == "0" || v == "false" {
		return nil
	}
	// only seed if no projects exist
	var cnt int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM projects`).Scan(&cnt); err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}
	id := fmt.Sprintf("seed-%d", time.Now().Unix())
	root := "."
	_, err := db.ExecContext(ctx, `INSERT INTO projects(id,name,root_path,created_at) VALUES(?,?,?,?)`, id, "demo", root, time.Now().Format(time.RFC3339))
	return err
}
