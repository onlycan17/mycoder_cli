package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// Migrator applies minimal schema for core entities. Caller provides opened *sql.DB.
type Migrator struct{}

func (m Migrator) Up(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS projects (
            id TEXT PRIMARY KEY,
            name TEXT NOT NULL,
            root_path TEXT NOT NULL,
            created_at TEXT NOT NULL
        );`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_name_root ON projects(name, root_path);`,
		`CREATE TABLE IF NOT EXISTS documents (
            id TEXT PRIMARY KEY,
            project_id TEXT NOT NULL,
            path TEXT NOT NULL,
            sha TEXT,
            lang TEXT,
            mtime TEXT,
            created_at TEXT NOT NULL,
            updated_at TEXT,
            FOREIGN KEY(project_id) REFERENCES projects(id)
        );`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_project_path ON documents(project_id, path);`,
		`CREATE TABLE IF NOT EXISTS chunks (
            id TEXT PRIMARY KEY,
            doc_id TEXT NOT NULL,
            ord INTEGER NOT NULL,
            text TEXT NOT NULL,
            token_count INTEGER,
            start_line INTEGER,
            end_line INTEGER,
            created_at TEXT NOT NULL,
            FOREIGN KEY(doc_id) REFERENCES documents(id)
        );`,
		// FTS5 lexical index (contentless pattern); app can choose to use it.
		`CREATE VIRTUAL TABLE IF NOT EXISTS termindex USING fts5(
            doc_id, ord, text,
            tokenize = 'unicode61 remove_diacritics 2'
        );`,
		`CREATE TABLE IF NOT EXISTS runs (
            id TEXT PRIMARY KEY,
            project_id TEXT NOT NULL,
            type TEXT NOT NULL,
            status TEXT NOT NULL,
            started_at TEXT NOT NULL,
            finished_at TEXT,
            metrics TEXT,
            logs_ref TEXT
        );`,
		`CREATE TABLE IF NOT EXISTS conversations (
            id TEXT PRIMARY KEY,
            project_id TEXT NOT NULL,
            title TEXT,
            pinned INTEGER DEFAULT 0,
            created_at TEXT NOT NULL,
            updated_at TEXT
        );`,
		`CREATE TABLE IF NOT EXISTS conversation_messages (
            id TEXT PRIMARY KEY,
            conv_id TEXT NOT NULL,
            role TEXT NOT NULL,
            content TEXT NOT NULL,
            token_count INTEGER,
            created_at TEXT NOT NULL
        );`,
		`CREATE TABLE IF NOT EXISTS conversation_summaries (
            id TEXT PRIMARY KEY,
            conv_id TEXT NOT NULL,
            version INTEGER NOT NULL,
            text TEXT NOT NULL,
            token_count INTEGER,
            updated_at TEXT
        );`,
		`CREATE TABLE IF NOT EXISTS execution_logs (
            id TEXT PRIMARY KEY,
            run_id TEXT NOT NULL,
            kind TEXT NOT NULL,
            payload_ref TEXT,
            started_at TEXT NOT NULL,
            finished_at TEXT,
            exit_code INTEGER
        );`,
		`CREATE TABLE IF NOT EXISTS knowledge (
            id TEXT PRIMARY KEY,
            project_id TEXT NOT NULL,
            source_type TEXT NOT NULL,
            path_or_url TEXT,
            title TEXT,
            text TEXT NOT NULL,
            trust_score REAL DEFAULT 0,
            pinned INTEGER DEFAULT 0,
            commit_sha TEXT,
            files TEXT,
            symbols TEXT,
            tags TEXT,
            created_at TEXT NOT NULL,
            verified_at TEXT,
            FOREIGN KEY(project_id) REFERENCES projects(id)
        );`,
		// embeddings: store provider/model/dim and vector(json) per chunk/doc
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
		// patches: record planned/applied patch hunks as json
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
		// symbols: code symbols with ranges for navigation
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
			return fmt.Errorf("migrate step %d: %w", i, err)
		}
	}
	// best-effort add columns for existing DBs
	_, _ = db.ExecContext(ctx, `ALTER TABLE chunks ADD COLUMN start_line INTEGER`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE chunks ADD COLUMN end_line INTEGER`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE documents ADD COLUMN mtime TEXT`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE knowledge ADD COLUMN commit_sha TEXT`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE knowledge ADD COLUMN files TEXT`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE knowledge ADD COLUMN symbols TEXT`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE knowledge ADD COLUMN tags TEXT`)
	return nil
}
