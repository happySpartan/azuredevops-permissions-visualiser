package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ResourcePermissionDetail is the resource-side access explorer result.
// It shows which subjects have assignments on a single secured resource.
type ResourcePermissionDetail struct {
	Resource PermissionResource `json:"resource"`
	Subjects []SubjectEntry     `json:"subjects"`
}

// SubjectEntry holds a subject and its decoded permissions for one resource.
type SubjectEntry struct {
	Subject     Subject            `json:"subject"`
	Permissions []PermissionResult `json:"permissions"`
}

// ResourcePermissionsByRun returns Azure DevOps' reported effective permission
// masks for one secured resource, showing every subject with an assignment on
// this exact token. It does not independently recompute Azure DevOps' verdict.
func (s *Store) ResourcePermissionsByRun(ctx context.Context, runID int64, token string) (*ResourcePermissionDetail, error) {
	resource, err := s.permissionResource(ctx, runID, token)
	if err != nil {
		return nil, err
	}
	ns := resource.Namespace

	byNamespace, err := namespaceActionsByRun(ctx, s.db, runID)
	if err != nil {
		return nil, err
	}
	actions := permissionActionsForNamespace(byNamespace, ns)

	rows, err := s.db.QueryContext(ctx, `
		SELECT a.descriptor,
		       a.allow_bitmask, a.deny_bitmask,
		       a.inherited_allow_bitmask, a.inherited_deny_bitmask,
		       a.effective_allow_bitmask, a.effective_deny_bitmask,
		       s.display_name, s.subject_kind, s.origin
		FROM assignments a
		JOIN subjects s ON s.run_id = a.run_id AND s.descriptor = a.descriptor
		WHERE a.run_id = ? AND a.namespace = ? AND a.security_token = ?
		ORDER BY a.descriptor`, runID, ns, token)
	if err != nil {
		return nil, fmt.Errorf("store: resource permissions: %w", err)
	}
	defer rows.Close()

	var subjects []SubjectEntry
	for rows.Next() {
		var descriptor string
		var allow, deny, inheritedAllow, inheritedDeny, effectiveAllow, effectiveDeny int64
		var displayName, kind, origin sql.NullString

		if err := rows.Scan(&descriptor, &allow, &deny,
			&inheritedAllow, &inheritedDeny,
			&effectiveAllow, &effectiveDeny,
			&displayName, &kind, &origin); err != nil {
			return nil, fmt.Errorf("store: resource scan: %w", err)
		}

		subject := Subject{
			Descriptor:  descriptor,
			DisplayName: displayName.String,
			Kind:        kind.String,
			Origin:      origin.String,
		}
		entry := SubjectEntry{Subject: subject}

		for _, action := range actions {
			state := PermissionNotSet
			if effectiveDeny&action.Bit != 0 {
				state = PermissionDeny
			} else if effectiveAllow&action.Bit != 0 {
				state = PermissionAllow
			}
			direct := allow&action.Bit != 0 || deny&action.Bit != 0
			inherited := inheritedAllow&action.Bit != 0 || inheritedDeny&action.Bit != 0
			entry.Permissions = append(entry.Permissions, PermissionResult{
				Bit: action.Bit, Name: action.Name, DisplayName: action.DisplayName,
				State: state, Direct: direct, Inherited: inherited,
				ViaGroup: state != PermissionNotSet && !direct && !inherited,
			})
		}
		subjects = append(subjects, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	if len(subjects) == 0 {
		return nil, ErrNotFound
	}

	return &ResourcePermissionDetail{
		Resource: resource,
		Subjects: subjects,
	}, nil
}
