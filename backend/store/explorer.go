package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ResourceProject is a project and its collected pipeline resources.
type ResourceProject struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Folders   []ResourceFolder   `json:"folders"`
	Pipelines []ResourcePipeline `json:"pipelines"`
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

// ResourcesByRun returns the collected resource hierarchy for an analysis run.
func (s *Store) ResourcesByRun(ctx context.Context, runID int64) ([]ResourceProject, error) {
	projects, err := s.ProjectsByRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	out := make([]ResourceProject, 0, len(projects))
	byID := make(map[string]int, len(projects))
	for _, project := range projects {
		byID[project.OrgID] = len(out)
		out = append(out, ResourceProject{
			ID:        project.OrgID,
			Name:      project.Name,
			Folders:   []ResourceFolder{},
			Pipelines: []ResourcePipeline{},
		})
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
	return out, pipelineRows.Err()
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
