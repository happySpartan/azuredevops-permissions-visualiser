package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// PermissionState is Azure DevOps' effective result for one action.
type PermissionState string

const (
	PermissionAllow  PermissionState = "allow"
	PermissionDeny   PermissionState = "deny"
	PermissionNotSet PermissionState = "notSet"
)

// SubjectPermissionDetail is the subject-side access explorer result.
type SubjectPermissionDetail struct {
	Subject   Subject              `json:"subject"`
	Resources []PermissionResource `json:"resources"`
}

// PermissionResource identifies a collected secured resource and its results.
type PermissionResource struct {
	Token       string             `json:"token"`
	Namespace   string             `json:"namespace"`
	Type        string             `json:"type"`
	Name        string             `json:"name"`
	ProjectID   string             `json:"projectId"`
	ProjectName string             `json:"projectName"`
	Path        string             `json:"path"`
	Permissions []PermissionResult `json:"permissions"`
}

// PermissionResult is one Azure DevOps Build action result and its provenance.
type PermissionResult struct {
	Bit         int64           `json:"bit"`
	Name        string          `json:"name"`
	DisplayName string          `json:"displayName"`
	State       PermissionState `json:"state"`
	Direct      bool            `json:"direct"`
	Inherited   bool            `json:"inherited"`
	ViaGroup    bool            `json:"viaGroup"`
}

// SubjectPermissionsByRun returns Azure DevOps' reported effective permission
// masks for a subject, decoded against the actions collected from the Build
// security namespace. It does not independently recompute Azure DevOps' verdict.
func (s *Store) SubjectPermissionsByRun(ctx context.Context, runID int64, descriptor string) (*SubjectPermissionDetail, error) {
	var subject Subject
	err := s.db.QueryRowContext(ctx, `
		SELECT descriptor, display_name, origin, subject_kind
		FROM subjects WHERE run_id=? AND descriptor=?`, runID, descriptor).
		Scan(&subject.Descriptor, &subject.DisplayName, &subject.Origin, &subject.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: subject detail: %w", err)
	}

	byNamespace, err := namespaceActionsByRun(ctx, s.db, runID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT namespace, security_token, allow_bitmask, deny_bitmask,
		       inherited_allow_bitmask, inherited_deny_bitmask,
		       effective_allow_bitmask, effective_deny_bitmask
		FROM assignments WHERE run_id=? AND descriptor=?
		ORDER BY security_token`, runID, descriptor)
	if err != nil {
		return nil, fmt.Errorf("store: subject permissions: %w", err)
	}
	defer rows.Close()

	type assignmentMasks struct {
		namespace                                                               string
		token                                                                   string
		allow, deny, inheritedAllow, inheritedDeny, effectiveAllow, effectiveDeny int64
	}
	assignments := []assignmentMasks{}
	for rows.Next() {
		var assignment assignmentMasks
		if err := rows.Scan(&assignment.namespace, &assignment.token,
			&assignment.allow, &assignment.deny,
			&assignment.inheritedAllow, &assignment.inheritedDeny,
			&assignment.effectiveAllow, &assignment.effectiveDeny); err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	resources := []PermissionResource{}
	for _, assignment := range assignments {
		resource, err := s.permissionResource(ctx, runID, assignment.token)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		for _, action := range permissionActionsForNamespace(byNamespace, assignment.namespace) {
			state := PermissionNotSet
			if assignment.effectiveDeny&action.Bit != 0 {
				state = PermissionDeny
			} else if assignment.effectiveAllow&action.Bit != 0 {
				state = PermissionAllow
			}
			direct := assignment.allow&action.Bit != 0 || assignment.deny&action.Bit != 0
			inherited := assignment.inheritedAllow&action.Bit != 0 || assignment.inheritedDeny&action.Bit != 0
			resource.Permissions = append(resource.Permissions, PermissionResult{
				Bit: action.Bit, Name: action.Name, DisplayName: action.DisplayName,
				State: state, Direct: direct, Inherited: inherited,
				ViaGroup: state != PermissionNotSet && !direct && !inherited,
			})
		}
		resources = append(resources, resource)
	}
	return &SubjectPermissionDetail{Subject: subject, Resources: resources}, nil
}

type permissionAction struct {
	Bit         int64
	Name        string
	DisplayName string
}

// namespaceActionsByRun loads all collected permission actions for a run,
// keyed by security namespace. Action bits are namespace-specific (for example
// bit 1 is ViewBuilds in Build but Administer in Git), so callers must decode
// each assignment against the actions of its own namespace.
func namespaceActionsByRun(ctx context.Context, db *sql.DB, runID int64) (map[string][]permissionAction, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT namespace, bit, name, display_name FROM permission_actions
		WHERE run_id=? ORDER BY namespace, bit`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: permission actions: %w", err)
	}
	defer rows.Close()
	byNamespace := map[string][]permissionAction{}
	var ns string
	for rows.Next() {
		var action permissionAction
		if err := rows.Scan(&ns, &action.Bit, &action.Name, &action.DisplayName); err != nil {
			return nil, err
		}
		byNamespace[ns] = append(byNamespace[ns], action)
	}
	return byNamespace, rows.Err()
}

// permissionActionsForNamespace returns the actions for one namespace from a
// run's grouped actions, defaulting to the Build actions when the namespace is
// unknown or absent.
func permissionActionsForNamespace(byNamespace map[string][]permissionAction, ns string) []permissionAction {
	if actions, ok := byNamespace[ns]; ok {
		return actions
	}
	if ns != NamespaceBuild {
		if actions, ok := byNamespace[NamespaceBuild]; ok {
			return actions
		}
	}
	return nil
}

func (s *Store) permissionResource(ctx context.Context, runID int64, token string) (PermissionResource, error) {
	parts := strings.Split(token, "/")
	first := parts[0]

	// Git namespace: repository GUIDs (bare repo tokens) and branch tokens
	// ("<repoGUID>/refs/heads/<branch>"). Repo GUIDs never collide with
	// project GUIDs; branch tokens always contain "/refs/".
	if strings.Contains(token, "/refs/") {
		return s.gitPermissionResource(ctx, runID, token)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM repositories WHERE run_id=? AND repository_id=?`, runID, first).Scan(&n); err == nil && n > 0 {
		return s.gitPermissionResource(ctx, runID, token)
	}

	// Build namespace: project, folder, and pipeline tokens.
	resource := PermissionResource{Token: token, Namespace: NamespaceBuild, ProjectID: first, Type: "project"}
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM projects WHERE run_id=? AND org_id=?`, runID, first).Scan(&resource.ProjectName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return resource, ErrNotFound
		}
		return resource, fmt.Errorf("store: permission project: %w", err)
	}
	resource.Name = resource.ProjectName

	// Build namespace: match a pipeline token using its stored definition id
	// and folder path.
	var pipelineName, folderPath string
	pipelineRows, queryErr := s.db.QueryContext(ctx, `
		SELECT definition_id, name, folder_path FROM pipelines WHERE run_id=? AND project_id=?`, runID, first)
	if queryErr != nil {
		return resource, queryErr
	}
	for pipelineRows.Next() {
		var definitionID int
		if scanErr := pipelineRows.Scan(&definitionID, &pipelineName, &folderPath); scanErr != nil {
			pipelineRows.Close()
			return resource, scanErr
		}
		if BuildToken(first, strings.TrimPrefix(folderPath, "/"), definitionID) == token {
			pipelineRows.Close()
			resource.Type, resource.Name, resource.Path = "pipeline", pipelineName, folderPath
			return resource, nil
		}
	}
	pipelineRows.Close()

	if len(parts) > 1 {
		resource.Type = "folder"
		resource.Path = "/" + strings.Join(parts[1:], "/")
		resource.Name = resource.Path
	}
	return resource, nil
}

// gitPermissionResource resolves a Git-namespace token to a repository or a
// branch. Repository tokens are the bare repository GUID; branch tokens are
// "<repoGUID>/refs/heads/<branch>".
func (s *Store) gitPermissionResource(ctx context.Context, runID int64, token string) (PermissionResource, error) {
	parts := strings.Split(token, "/")
	repoID := parts[0]

	var repoName, projectID string
	if err := s.db.QueryRowContext(ctx, `
		SELECT r.name, r.project_id FROM repositories r WHERE r.run_id=? AND r.repository_id=?`, runID, repoID).
		Scan(&repoName, &projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PermissionResource{}, ErrNotFound
		}
		return PermissionResource{}, fmt.Errorf("store: permission repository: %w", err)
	}

	var projectName string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM projects WHERE run_id=? AND org_id=?`, runID, projectID).Scan(&projectName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PermissionResource{}, ErrNotFound
		}
		return PermissionResource{}, fmt.Errorf("store: permission repository project: %w", err)
	}

	resource := PermissionResource{
		Token: token, Namespace: NamespaceGit,
		ProjectID: projectID, ProjectName: projectName,
	}
	if len(parts) == 1 {
		resource.Type = "repository"
		resource.Name = repoName
		return resource, nil
	}

	// Branch token: <repoGUID>/refs/heads/<branch>.
	resource.Type = "branch"
	resource.Name = parts[len(parts)-1]
	resource.Path = "/" + strings.Join(parts[1:], "/")
	return resource, nil
}
