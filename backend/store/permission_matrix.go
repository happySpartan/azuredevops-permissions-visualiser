package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// MatrixAction identifies the single permission action displayed by a scoped matrix.
type MatrixAction struct {
	Bit         int64  `json:"bit"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// PermissionMatrix is a project-scoped comparison of subjects and secured resources.
type PermissionMatrix struct {
	ProjectID   string       `json:"projectId"`
	ProjectName string       `json:"projectName"`
	Action      MatrixAction `json:"action"`
	Subjects    []Subject    `json:"subjects"`
	Rows        []MatrixRow  `json:"rows"`
}

// MatrixRow contains the collected cells for one secured resource.
type MatrixRow struct {
	Resource PermissionResource           `json:"resource"`
	Cells    map[string]*PermissionResult `json:"cells"`
}

// PermissionMatrixByRun returns one action across assignments in one project.
// Missing subject/resource assignments are omitted, while a collected assignment
// whose effective mask does not contain the action is represented as not set.
func (s *Store) PermissionMatrixByRun(ctx context.Context, runID int64, projectID string, bit int64) (*PermissionMatrix, error) {
	var projectName string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM projects WHERE run_id=? AND org_id=?`, runID, projectID).Scan(&projectName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: matrix project: %w", err)
	}

	var action MatrixAction
	if err := s.db.QueryRowContext(ctx, `
		SELECT bit, name, display_name FROM permission_actions
		WHERE run_id=? AND namespace=? AND bit=?`, runID, NamespaceBuild, bit).Scan(&action.Bit, &action.Name, &action.DisplayName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: matrix action: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT a.security_token, a.descriptor,
		       a.allow_bitmask, a.deny_bitmask,
		       a.inherited_allow_bitmask, a.inherited_deny_bitmask,
		       a.effective_allow_bitmask, a.effective_deny_bitmask,
		       s.display_name, s.subject_kind, s.origin
		FROM assignments a
		JOIN subjects s ON s.run_id=a.run_id AND s.descriptor=a.descriptor
		WHERE a.run_id=? AND a.namespace=? AND (a.security_token=? OR a.security_token LIKE ?)
		ORDER BY a.security_token, s.display_name, a.descriptor`, runID, NamespaceBuild, projectID, projectID+"/%")
	if err != nil {
		return nil, fmt.Errorf("store: matrix assignments: %w", err)
	}
	defer rows.Close()

	type assignment struct {
		token, descriptor, displayName, kind, origin                              string
		allow, deny, inheritedAllow, inheritedDeny, effectiveAllow, effectiveDeny int64
	}
	var assignments []assignment
	for rows.Next() {
		var item assignment
		if err := rows.Scan(&item.token, &item.descriptor, &item.allow, &item.deny,
			&item.inheritedAllow, &item.inheritedDeny, &item.effectiveAllow, &item.effectiveDeny,
			&item.displayName, &item.kind, &item.origin); err != nil {
			return nil, fmt.Errorf("store: matrix scan: %w", err)
		}
		assignments = append(assignments, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	subjects := map[string]Subject{}
	matrixRows := map[string]*MatrixRow{}
	var tokens []string
	for _, assignment := range assignments {
		subjects[assignment.descriptor] = Subject{Descriptor: assignment.descriptor, DisplayName: assignment.displayName, Kind: assignment.kind, Origin: assignment.origin}
		row := matrixRows[assignment.token]
		if row == nil {
			resource, err := s.permissionResource(ctx, runID, assignment.token)
			if err != nil {
				return nil, err
			}
			row = &MatrixRow{Resource: resource, Cells: map[string]*PermissionResult{}}
			matrixRows[assignment.token] = row
			tokens = append(tokens, assignment.token)
		}
		state := PermissionNotSet
		if assignment.effectiveDeny&bit != 0 {
			state = PermissionDeny
		} else if assignment.effectiveAllow&bit != 0 {
			state = PermissionAllow
		}
		direct := assignment.allow&bit != 0 || assignment.deny&bit != 0
		inherited := assignment.inheritedAllow&bit != 0 || assignment.inheritedDeny&bit != 0
		row.Cells[assignment.descriptor] = &PermissionResult{
			Bit: bit, Name: action.Name, DisplayName: action.DisplayName,
			State: state, Direct: direct, Inherited: inherited,
			ViaGroup: state != PermissionNotSet && !direct && !inherited,
		}
	}
	result := &PermissionMatrix{ProjectID: projectID, ProjectName: projectName, Action: action}
	for _, subject := range subjects {
		result.Subjects = append(result.Subjects, subject)
	}
	sortSubjects(result.Subjects)
	for _, token := range tokens {
		result.Rows = append(result.Rows, *matrixRows[token])
	}
	if err := s.addEmptyMatrixResources(ctx, runID, projectID, matrixRows, &result.Rows); err != nil {
		return nil, err
	}
	sortMatrixRows(result.Rows)
	return result, nil
}

func (s *Store) addEmptyMatrixResources(ctx context.Context, runID int64, projectID string, existing map[string]*MatrixRow, rows *[]MatrixRow) error {
	addToken := func(token string) error {
		if existing[token] != nil {
			return nil
		}
		resource, err := s.permissionResource(ctx, runID, token)
		if err != nil {
			return err
		}
		*rows = append(*rows, MatrixRow{Resource: resource, Cells: map[string]*PermissionResult{}})
		existing[token] = &MatrixRow{}
		return nil
	}
	if err := addToken(projectID); err != nil {
		return err
	}

	folderRows, err := s.db.QueryContext(ctx, `SELECT path FROM folders WHERE run_id=? AND project_id=?`, runID, projectID)
	if err != nil {
		return err
	}
	var folderTokens []string
	for folderRows.Next() {
		var path string
		if err := folderRows.Scan(&path); err != nil {
			folderRows.Close()
			return err
		}
		folderTokens = append(folderTokens, projectID+"/"+strings.TrimPrefix(path, "/"))
	}
	if err := folderRows.Err(); err != nil {
		folderRows.Close()
		return err
	}
	folderRows.Close()
	for _, token := range folderTokens {
		if err := addToken(token); err != nil {
			return err
		}
	}

	pipelineRows, err := s.db.QueryContext(ctx, `SELECT definition_id, folder_path FROM pipelines WHERE run_id=? AND project_id=?`, runID, projectID)
	if err != nil {
		return err
	}
	var pipelineTokens []string
	for pipelineRows.Next() {
		var definitionID int
		var folderPath string
		if err := pipelineRows.Scan(&definitionID, &folderPath); err != nil {
			pipelineRows.Close()
			return err
		}
		pipelineTokens = append(pipelineTokens, BuildToken(projectID, strings.TrimPrefix(folderPath, "/"), definitionID))
	}
	if err := pipelineRows.Err(); err != nil {
		pipelineRows.Close()
		return err
	}
	pipelineRows.Close()
	for _, token := range pipelineTokens {
		if err := addToken(token); err != nil {
			return err
		}
	}
	return nil
}

func sortSubjects(subjects []Subject) {
	for i := 1; i < len(subjects); i++ {
		for j := i; j > 0 && strings.ToLower(subjects[j].DisplayName) < strings.ToLower(subjects[j-1].DisplayName); j-- {
			subjects[j], subjects[j-1] = subjects[j-1], subjects[j]
		}
	}
}

func sortMatrixRows(rows []MatrixRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].Resource.Token < rows[j-1].Resource.Token; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}
