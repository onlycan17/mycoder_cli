package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestRunsAndExecutionLogs(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "exec.db")
	s, err := NewSQLite(dbpath)
	if err != nil {
		t.Skip("sqlite not available:", err)
	}

	// create a project
	p := s.CreateProject("proj-exec", dir, nil)

	// create a run
	run, err := s.CreateRun(p.ID, "hooks", "running")
	if err != nil {
		t.Fatalf("CreateRun error: %v", err)
	}
	if run.ID == "" || run.ProjectID != p.ID || run.Type != "hooks" || run.Status != "running" {
		t.Fatalf("unexpected run: %+v", run)
	}

	// add one execution log
	x, err := s.AddExecutionLog(run.ID, "hook", "payload://test", 0)
	if err != nil {
		t.Fatalf("AddExecutionLog error: %v", err)
	}
	if x.RunID != run.ID || x.Kind != "hook" || x.ExitCode != 0 || x.FinishedAt == nil {
		t.Fatalf("unexpected execution log: %+v", x)
	}

	// list execution logs
	logs, err := s.ListExecutionLogs(run.ID)
	if err != nil {
		t.Fatalf("ListExecutionLogs error: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	// finish run and verify persisted fields
	if err := s.FinishRun(run.ID, "completed", "{}", "ref://logs"); err != nil {
		t.Fatalf("FinishRun error: %v", err)
	}
	var status, logsRef sql.NullString
	if err := s.db.QueryRow(`SELECT status, logs_ref FROM runs WHERE id=?`, run.ID).Scan(&status, &logsRef); err != nil {
		t.Fatalf("query run after finish: %v", err)
	}
	if !status.Valid || status.String != "completed" {
		t.Fatalf("unexpected status: %v", status)
	}
	if !logsRef.Valid || logsRef.String != "ref://logs" {
		t.Fatalf("unexpected logs_ref: %v", logsRef)
	}
}
