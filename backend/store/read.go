package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Project is a project row persisted for a run.
type Project struct {
	OrgID string
	Name  string
}

// ProjectsByRun lists the projects collected in a run.
func (s *Store) ProjectsByRun(ctx context.Context, runID int64) ([]Project, error) {
	return projectsQuery(ctx, s.db, runID)
}

// ProjectsByRunTx lists the projects written so far within an open run
// transaction. It uses the transaction's own connection so it sees the run's
// uncommitted writes and does not deadlock against MaxOpenConns(1).
func (t *Tx) ProjectsByRunTx(ctx context.Context) ([]Project, error) {
	return projectsQuery(ctx, t.tx, t.runID)
}

// queryer is satisfied by both *sql.DB and *sql.Tx.
type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func projectsQuery(ctx context.Context, q queryer, runID int64) ([]Project, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT org_id, name FROM projects WHERE run_id=? ORDER BY name`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: projects by run: %w", err)
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.OrgID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// TokensByRun returns the distinct Build-namespace security tokens for a run:
// project-level tokens, folder tokens, and pipeline-definition tokens (with any
// folder path prefix). These are the tokens used to query ACLs for the run.
func (s *Store) TokensByRun(ctx context.Context, runID int64) ([]string, error) {
	return tokensQuery(ctx, s.db, runID)
}

// TokensByRunTx returns the run's security tokens within the open run
// transaction (using its connection, seeing uncommitted writes).
func (t *Tx) TokensByRunTx(ctx context.Context) ([]string, error) {
	return tokensQuery(ctx, t.tx, t.runID)
}

func tokensQuery(ctx context.Context, q queryer, runID int64) ([]string, error) {
	tok := map[string]bool{}

	rows, err := q.QueryContext(ctx,
		`SELECT org_id FROM projects WHERE run_id=?`, runID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pid string
		if err := rows.Scan(&pid); err != nil {
			rows.Close()
			return nil, err
		}
		tok[pid] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.QueryContext(ctx,
		`SELECT project_id, path FROM folders WHERE run_id=?`, runID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pid, path string
		if err := rows.Scan(&pid, &path); err != nil {
			rows.Close()
			return nil, err
		}
		tok[BuildToken(pid, strings.TrimPrefix(path, "/"), 0)] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.QueryContext(ctx,
		`SELECT project_id, definition_id, folder_path FROM pipelines WHERE run_id=?`, runID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pid, folder string
		var def int
		if err := rows.Scan(&pid, &def, &folder); err != nil {
			rows.Close()
			return nil, err
		}
		tok[BuildToken(pid, strings.TrimPrefix(folder, "/"), def)] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(tok))
	for t := range tok {
		out = append(out, t)
	}
	return out, nil
}

// BuildToken assembles a Build-namespace security token for a project, optional
// folder path, and optional definition id. Mirrors azdo.BuildSecurityToken but
// lives here to avoid a store -> azdo import.
func BuildToken(projectID, folderPath string, definitionID int) string {
	s := projectID
	if folderPath != "" {
		s += "/" + strings.TrimPrefix(folderPath, "/")
	}
	if definitionID > 0 {
		s += "/" + fmt.Sprint(definitionID)
	}
	return s
}
