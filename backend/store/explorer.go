package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ResourceProject is a project and its collected resources.
type ResourceProject struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Folders        []ResourceFolder     `json:"folders"`
	Pipelines      []ResourcePipeline   `json:"pipelines"`
	Repositories   []ResourceRepository `json:"repositories"`
	Endpoints      []ResourceEndpoint   `json:"endpoints"`
	VariableGroups []ResourceVarGroup   `json:"variableGroups"`
}

// ResourceFolder is a collected pipeline folder.
type ResourceFolder struct {
	Path string `json:"path"`
}

// ResourcePipeline is a collected YAML pipeline definition.
type ResourcePipeline struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	FolderPath  string `json:"folderPath"`
	QueueStatus string `json:"queueStatus"`
}

// ResourceRepository is a collected Git repository (Git namespace).
type ResourceRepository struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	DefaultBranch string           `json:"defaultBranch"`
	Branches      []ResourceBranch `json:"branches"`
}

// ResourceBranch is a collected branch within a Git repository.
type ResourceBranch struct {
	Name string `json:"name"` // full ref, e.g. refs/heads/main
}

// ResourceEndpoint is a collected service connection (ServiceEndpoints).
type ResourceEndpoint struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ResourceVarGroup is a collected library variable group (Library).
type ResourceVarGroup struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ResourceAgentPool is a collected organization-level agent pool
// (BuildAdministration). It is returned at the top level, not per project.
type ResourceAgentPool struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	IsHosted bool   `json:"isHosted"`
}

// ResourceHierarchy is the top-level response from the resources explorer.
type ResourceHierarchy struct {
	AgentPools []ResourceAgentPool `json:"agentPools"`
	Projects   []ResourceProject   `json:"projects"`
}

// ResourcesByRun returns the collected resource hierarchy for an analysis run.
// Agent pools are organization-level; everything else is per project.
func (s *Store) ResourcesByRun(ctx context.Context, runID int64) (*ResourceHierarchy, error) {
	hierarchy := &ResourceHierarchy{AgentPools: []ResourceAgentPool{}}
	projects, err := s.ProjectsByRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	out := make([]ResourceProject, 0, len(projects))
	byID := make(map[string]int, len(projects))
	for _, project := range projects {
		byID[project.OrgID] = len(out)
		out = append(out, ResourceProject{
			ID:             project.OrgID,
			Name:           project.Name,
			Folders:        []ResourceFolder{},
			Pipelines:      []ResourcePipeline{},
			Repositories:   []ResourceRepository{},
			Endpoints:      []ResourceEndpoint{},
			VariableGroups: []ResourceVarGroup{},
		})
	}

	poolRows, err := s.db.QueryContext(ctx,
		`SELECT pool_id, name, is_hosted FROM agent_pools WHERE run_id=? ORDER BY name`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: resources agent pools: %w", err)
	}
	defer poolRows.Close()
	for poolRows.Next() {
		var pool ResourceAgentPool
		var hosted int
		if err := poolRows.Scan(&pool.ID, &pool.Name, &hosted); err != nil {
			return nil, err
		}
		pool.IsHosted = hosted != 0
		hierarchy.AgentPools = append(hierarchy.AgentPools, pool)
	}
	if err := poolRows.Err(); err != nil {
		return nil, err
	}

	folderRows, err := s.db.QueryContext(ctx,
		`SELECT project_id, path FROM folders WHERE run_id=? ORDER BY path`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: resources folders: %w", err)
	}
	defer folderRows.Close()
	for folderRows.Next() {
		var projectID, path string
		if err := folderRows.Scan(&projectID, &path); err != nil {
			return nil, err
		}
		if index, ok := byID[projectID]; ok {
			out[index].Folders = append(out[index].Folders, ResourceFolder{Path: path})
		}
	}
	if err := folderRows.Err(); err != nil {
		return nil, err
	}

	pipelineRows, err := s.db.QueryContext(ctx, `
		SELECT project_id, definition_id, name, folder_path, queue_status
		FROM pipelines WHERE run_id=? ORDER BY name, definition_id`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: resources pipelines: %w", err)
	}
	defer pipelineRows.Close()
	for pipelineRows.Next() {
		var projectID string
		var pipeline ResourcePipeline
		if err := pipelineRows.Scan(&projectID, &pipeline.ID, &pipeline.Name, &pipeline.FolderPath, &pipeline.QueueStatus); err != nil {
			return nil, err
		}
		if index, ok := byID[projectID]; ok {
			out[index].Pipelines = append(out[index].Pipelines, pipeline)
		}
	}
	if err := pipelineRows.Err(); err != nil {
		return nil, err
	}

	repoRows, err := s.db.QueryContext(ctx, `
		SELECT repository_id, name, project_id, default_branch
		FROM repositories WHERE run_id=? ORDER BY name`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: resources repositories: %w", err)
	}
	defer repoRows.Close()
	repoByID := map[string]ResourceRepository{}
	repoIndex := map[string]int{}
	for repoRows.Next() {
		var repo ResourceRepository
		var projectID string
		if err := repoRows.Scan(&repo.ID, &repo.Name, &projectID, &repo.DefaultBranch); err != nil {
			return nil, err
		}
		repo.Branches = []ResourceBranch{}
		repoByID[repo.ID] = repo
		repoIndex[repo.ID] = len(repoByID) - 1
		if index, ok := byID[projectID]; ok {
			out[index].Repositories = append(out[index].Repositories, repo)
		}
	}
	if err := repoRows.Err(); err != nil {
		return nil, err
	}

	branchRows, err := s.db.QueryContext(ctx, `
		SELECT repository_id, name FROM branches WHERE run_id=? ORDER BY name`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: resources branches: %w", err)
	}
	defer branchRows.Close()
	for branchRows.Next() {
		var repoID, name string
		if err := branchRows.Scan(&repoID, &name); err != nil {
			return nil, err
		}
		for i, repo := range out {
			for j, r := range repo.Repositories {
				if r.ID == repoID {
					out[i].Repositories[j].Branches = append(out[i].Repositories[j].Branches, ResourceBranch{Name: name})
				}
			}
		}
	}
	if err := branchRows.Err(); err != nil {
		return nil, err
	}

	endpointRows, err := s.db.QueryContext(ctx, `
		SELECT project_id, endpoint_id, name, endpoint_type
		FROM service_endpoints WHERE run_id=? ORDER BY name`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: resources endpoints: %w", err)
	}
	defer endpointRows.Close()
	for endpointRows.Next() {
		var projectID, endpointID, name, endpointType string
		if err := endpointRows.Scan(&projectID, &endpointID, &name, &endpointType); err != nil {
			return nil, err
		}
		if index, ok := byID[projectID]; ok {
			out[index].Endpoints = append(out[index].Endpoints, ResourceEndpoint{ID: endpointID, Name: name, Type: endpointType})
		}
	}
	if err := endpointRows.Err(); err != nil {
		return nil, err
	}

	vgRows, err := s.db.QueryContext(ctx, `
		SELECT project_id, variable_group_id, name
		FROM variable_groups WHERE run_id=? ORDER BY name`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: resources variable groups: %w", err)
	}
	defer vgRows.Close()
	for vgRows.Next() {
		var projectID, name string
		var id int
		if err := vgRows.Scan(&projectID, &id, &name); err != nil {
			return nil, err
		}
		if index, ok := byID[projectID]; ok {
			out[index].VariableGroups = append(out[index].VariableGroups, ResourceVarGroup{ID: id, Name: name})
		}
	}
	if err := vgRows.Err(); err != nil {
		return nil, err
	}

	hierarchy.Projects = out
	return hierarchy, nil
}

// Subject is a user or group collected for an analysis run.
type Subject struct {
	Descriptor  string `json:"descriptor"`
	DisplayName string `json:"displayName"`
	Origin      string `json:"origin"`
	Kind        string `json:"kind"`
}

// SubjectQuery controls server-side filtering and pagination.
type SubjectQuery struct {
	Search string
	Kind   string
	Limit  int
	Offset int
}

// SubjectPage is one page of subjects and the filtered total.
type SubjectPage struct {
	Items  []Subject `json:"items"`
	Total  int       `json:"total"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
}

// SubjectsByRun searches subjects by display name or descriptor.
func (s *Store) SubjectsByRun(ctx context.Context, runID int64, query SubjectQuery) (*SubjectPage, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}

	where := []string{"run_id=?"}
	args := []any{runID}
	if search := strings.TrimSpace(query.Search); search != "" {
		where = append(where, `(display_name LIKE ? COLLATE NOCASE OR descriptor LIKE ? COLLATE NOCASE)`)
		pattern := "%" + search + "%"
		args = append(args, pattern, pattern)
	}
	if kind := strings.TrimSpace(query.Kind); kind != "" {
		where = append(where, "subject_kind=?")
		args = append(args, kind)
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subjects WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("store: count subjects: %w", err)
	}

	pageArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT descriptor, display_name, origin, subject_kind
		FROM subjects WHERE `+clause+`
		ORDER BY display_name COLLATE NOCASE, descriptor
		LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, fmt.Errorf("store: subjects: %w", err)
	}
	defer rows.Close()

	items := []Subject{}
	for rows.Next() {
		var subject Subject
		if err := rows.Scan(&subject.Descriptor, &subject.DisplayName, &subject.Origin, &subject.Kind); err != nil {
			return nil, err
		}
		items = append(items, subject)
	}
	if err := rows.Err(); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return &SubjectPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}
