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

// GitTokensByRunTx returns the distinct Git-namespace security tokens for the
// run: repository-level tokens and branch tokens (with their repo GUID prefix).
func (t *Tx) GitTokensByRunTx(ctx context.Context) ([]string, error) {
	return gitTokensQuery(ctx, t.tx, t.runID)
}

// PipelineResourceTokensByRunTx returns the distinct security tokens for the
// pipeline resource namespaces (BuildAdministration agent pools, ServiceEndpoints,
// and Library variable groups), keyed by namespace.
func (t *Tx) PipelineResourceTokensByRunTx(ctx context.Context) (map[string][]string, error) {
	tokens := map[string][]string{}
	appendToken := func(ns, token string) {
		tokens[ns] = append(tokens[ns], token)
	}

	rows, err := t.tx.QueryContext(ctx, `SELECT pool_id FROM agent_pools WHERE run_id=? ORDER BY pool_id`, t.runID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		appendToken(NamespaceBuildAdministration, "pools/"+fmt.Sprint(id))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = t.tx.QueryContext(ctx, `SELECT project_id, endpoint_id FROM service_endpoints WHERE run_id=? ORDER BY project_id, endpoint_id`, t.runID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var projectID, endpointID string
		if err := rows.Scan(&projectID, &endpointID); err != nil {
			rows.Close()
			return nil, err
		}
		appendToken(NamespaceServiceEndpoints, projectID+"/"+endpointID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = t.tx.QueryContext(ctx, `SELECT project_id, variable_group_id FROM variable_groups WHERE run_id=? ORDER BY project_id, variable_group_id`, t.runID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var projectID string
		var id int
		if err := rows.Scan(&projectID, &id); err != nil {
			rows.Close()
			return nil, err
		}
		appendToken(NamespaceLibrary, projectID+"/"+fmt.Sprint(id))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tokens, nil
}

func gitTokensQuery(ctx context.Context, q queryer, runID int64) ([]string, error) {
	tok := map[string]bool{}
	rows, err := q.QueryContext(ctx, `SELECT repository_id FROM repositories WHERE run_id=?`, runID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		tok[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.QueryContext(ctx, `SELECT repository_id, name FROM branches WHERE run_id=?`, runID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		tok[id+"/"+name] = true
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

// NamespaceBuild is the Build security namespace GUID, matching azdo.
const NamespaceBuild = "33344d9c-fc72-4d6f-aba5-fa317101a7e9"

// NamespaceGit is the Git Repositories security namespace GUID, matching azdo.
const NamespaceGit = "2e9eb7ed-3c0a-47d4-87c1-0ffdd275fd87"

// NamespaceBuildAdministration is the agent-pool security namespace GUID.
const NamespaceBuildAdministration = "302acaca-b667-436d-a946-87133492041c"

// NamespaceServiceEndpoints is the service-connection security namespace GUID.
const NamespaceServiceEndpoints = "49b48001-ca20-4adc-8111-5b60c903a50c"

// NamespaceLibrary is the variable-group security namespace GUID.
const NamespaceLibrary = "b7e84409-6553-448a-bbb2-af228e07cbeb"

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
