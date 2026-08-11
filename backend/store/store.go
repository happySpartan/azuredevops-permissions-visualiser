// Package store persists collected analysis runs in a local SQLite database.
//
// It honours the product decisions:
//
//   - Local persistence only (SQLite in a per-user data directory, configurable).
//   - "No half data": a run is committed as a single transaction only once every
//     required phase has succeeded. A failed or cancelled run never becomes an
//     analysis; the previous successful run is preserved.
//   - Retain only the latest completed run, replacing it after the next
//     successful run. A "delete collected data" action is provided.
//
// The store persists what Azure DevOps reported (the source of truth); it does
// not compute effective-permission verdicts.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a run does not exist.
var ErrNotFound = errors.New("store: not found")

// Status is the lifecycle state of an analysis run.
type Status string

const (
	StatusRunning   Status = "running"
	StatusComplete  Status = "complete"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Run is a point-in-time collection of one organization.
type Run struct {
	ID          int64
	Org         string
	Status      Status
	StartedAt   time.Time
	CompletedAt *time.Time
	Error       string
	// Counts captured at completion.
	ProjectCount    int
	FolderCount     int
	PipelineCount   int
	SubjectCount    int
	AssignmentCount int
}

// Store wraps a SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. It is the caller's responsibility to ensure the parent directory
// exists.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: create data directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: secure data directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // SQLite: serialise writers
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: secure database: %w", err)
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DefaultDataDir returns the per-user data directory per Q29.
func DefaultDataDir() string {
	if d := os.Getenv("AZDO_VIS_DATA_DIR"); d != "" {
		return d
	}
	if runtime := os.Getenv("LOCALAPPDATA"); runtime != "" {
		return filepath.Join(runtime, "AzureDevOpsPermsVisualiser")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".azdo-perms-visualiser"
	}
	return filepath.Join(home, ".local", "share", "azuredevops-permissions-visualiser")
}

// DefaultDBPath returns the default SQLite file location.
func DefaultDBPath() string {
	return filepath.Join(DefaultDataDir(), "visualiser.db")
}

// DB exposes the underlying handle (used by the collector within a transaction).
func (s *Store) DB() *sql.DB { return s.db }

// migrate creates the schema if needed. It is idempotent.
func (s *Store) migrate() error {
	const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS runs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    org            TEXT    NOT NULL,
    status         TEXT    NOT NULL,
    started_at     TEXT    NOT NULL,
    completed_at   TEXT,
    error          TEXT,
    project_count     INTEGER NOT NULL DEFAULT 0,
    folder_count      INTEGER NOT NULL DEFAULT 0,
    pipeline_count    INTEGER NOT NULL DEFAULT 0,
    subject_count     INTEGER NOT NULL DEFAULT 0,
    assignment_count  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS projects (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id     INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    org_id     TEXT    NOT NULL,
    name       TEXT    NOT NULL,
    UNIQUE(run_id, org_id)
);

CREATE TABLE IF NOT EXISTS folders (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    project_id  TEXT    NOT NULL,
    path        TEXT    NOT NULL,
    UNIQUE(run_id, project_id, path)
);

CREATE TABLE IF NOT EXISTS pipelines (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    project_id  TEXT    NOT NULL,
    definition_id INTEGER NOT NULL,
    name        TEXT    NOT NULL,
    folder_path TEXT    NOT NULL DEFAULT '',
    queue_status TEXT   NOT NULL DEFAULT '',
    UNIQUE(run_id, project_id, definition_id)
);

CREATE TABLE IF NOT EXISTS subjects (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    descriptor  TEXT    NOT NULL,
    display_name TEXT   NOT NULL,
    origin      TEXT    NOT NULL DEFAULT '',
    subject_kind TEXT   NOT NULL DEFAULT '',
    UNIQUE(run_id, descriptor)
);

CREATE TABLE IF NOT EXISTS memberships (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    parent_descriptor TEXT NOT NULL,
    member_descriptor  TEXT NOT NULL,
    UNIQUE(run_id, parent_descriptor, member_descriptor)
);

CREATE TABLE IF NOT EXISTS assignments (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id        INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    security_token TEXT   NOT NULL,
    descriptor    TEXT    NOT NULL,
    allow_bitmask  INTEGER NOT NULL,
    deny_bitmask   INTEGER NOT NULL,
    inherited      INTEGER NOT NULL DEFAULT 0,
    inherited_allow_bitmask INTEGER NOT NULL DEFAULT 0,
    inherited_deny_bitmask  INTEGER NOT NULL DEFAULT 0,
    effective_allow_bitmask INTEGER NOT NULL DEFAULT 0,
    effective_deny_bitmask  INTEGER NOT NULL DEFAULT 0,
    UNIQUE(run_id, security_token, descriptor)
);

CREATE TABLE IF NOT EXISTS permission_actions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id       INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    bit          INTEGER NOT NULL,
    name         TEXT    NOT NULL,
    display_name TEXT    NOT NULL,
    UNIQUE(run_id, bit)
);

CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
CREATE INDEX IF NOT EXISTS idx_projects_run ON projects(run_id);
CREATE INDEX IF NOT EXISTS idx_folders_run ON folders(run_id);
CREATE INDEX IF NOT EXISTS idx_pipelines_run ON pipelines(run_id);
CREATE INDEX IF NOT EXISTS idx_subjects_run ON subjects(run_id);
CREATE INDEX IF NOT EXISTS idx_memberships_run ON memberships(run_id);
CREATE INDEX IF NOT EXISTS idx_assignments_run ON assignments(run_id);
CREATE INDEX IF NOT EXISTS idx_permission_actions_run ON permission_actions(run_id);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	columns := []struct{ name, definition string }{
		{"inherited_allow_bitmask", "INTEGER NOT NULL DEFAULT 0"},
		{"inherited_deny_bitmask", "INTEGER NOT NULL DEFAULT 0"},
		{"effective_allow_bitmask", "INTEGER NOT NULL DEFAULT 0"},
		{"effective_deny_bitmask", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, column := range columns {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('assignments') WHERE name=?`, column.name).Scan(&count); err != nil {
			return fmt.Errorf("store: inspect assignments: %w", err)
		}
		if count == 0 {
			if _, err := s.db.Exec(`ALTER TABLE assignments ADD COLUMN ` + column.name + ` ` + column.definition); err != nil {
				return fmt.Errorf("store: add %s: %w", column.name, err)
			}
		}
	}
	return nil
}

// BeginRun starts a new run for the given organization and returns its ID.
// It does not touch any existing data: the previous run remains until this one
// is committed successfully.
func (s *Store) BeginRun(ctx context.Context, org string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (org, status, started_at) VALUES (?, ?, ?)`,
		org, StatusRunning, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("store: begin run: %w", err)
	}
	return res.LastInsertId()
}

// LatestRunID returns the id of the most recent completed run, if any. A run
// that is still running, failed, or cancelled is not the explorable analysis.
func (s *Store) LatestRunID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM runs WHERE status=? ORDER BY id DESC LIMIT 1`, StatusComplete).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

// RunByID loads a run by id.
func (s *Store) RunByID(ctx context.Context, id int64) (*Run, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, org, status, started_at, completed_at, error,
		       project_count, folder_count, pipeline_count, subject_count, assignment_count
		FROM runs WHERE id = ?`, id)
	r := &Run{}
	var started string
	var completed, errStr sql.NullString
	if err := row.Scan(&r.ID, &r.Org, &r.Status, &started, &completed, &errStr,
		&r.ProjectCount, &r.FolderCount, &r.PipelineCount, &r.SubjectCount, &r.AssignmentCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
	if completed.Valid {
		t, err := time.Parse(time.RFC3339Nano, completed.String)
		if err == nil {
			r.CompletedAt = &t
		}
	}
	if errStr.Valid {
		r.Error = errStr.String
	}
	return r, nil
}

// CompleteRun commits a run as successful and replaces the previous successful
// run. The whole replacement happens atomically: the new run's data is written
// and older successful runs are purged in the same transaction, so there is
// never a window with no valid run or with half a run visible.
func (s *Store) CompleteRun(ctx context.Context, runID int64, counts RunCounts) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE runs SET status=?, completed_at=?, project_count=?, folder_count=?,
			pipeline_count=?, subject_count=?, assignment_count=? WHERE id=?`,
		StatusComplete, time.Now().UTC().Format(time.RFC3339Nano),
		counts.Projects, counts.Folders, counts.Pipelines, counts.Subjects, counts.Assignments,
		runID); err != nil {
		return err
	}

	// Purge any other successful runs and their data (retain latest only).
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM runs WHERE status=? AND id<>?`, StatusComplete, runID); err != nil {
		return err
	}
	return tx.Commit()
}

// FailRun marks a run as failed or cancelled and discards its partial data.
// The previous successful run (if any) is untouched.
func (s *Store) FailRun(ctx context.Context, runID int64, status Status, msg string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE runs SET status=?, completed_at=?, error=? WHERE id=?`,
		status, time.Now().UTC().Format(time.RFC3339Nano), msg, runID); err != nil {
		return err
	}
	// Discard this run's data (no half data).
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE run_id=?`, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM folders WHERE run_id=?`, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pipelines WHERE run_id=?`, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM subjects WHERE run_id=?`, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memberships WHERE run_id=?`, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM assignments WHERE run_id=?`, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM permission_actions WHERE run_id=?`, runID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteAll removes every run and all collected data ("Delete collected data").
func (s *Store) DeleteAll(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM runs`)
	if err != nil {
		return fmt.Errorf("store: delete all: %w", err)
	}
	return nil
}

// RunCounts summarises a completed collection.
type RunCounts struct {
	Projects    int
	Folders     int
	Pipelines   int
	Subjects    int
	Assignments int
}
